// Package noise classifies values that legitimately differ between two
// healthy backends — timestamps, request ids, generated identifiers —
// so twinget can report parity instead of drowning real regressions in
// per-request churn. Every rule here is documented, with its rationale,
// in docs/noise-filters.md.
//
// The contract is conservative on purpose: a difference is only
// neutralized when BOTH sides independently match the SAME noise shape.
// A UUID on one side and "banana" on the other is a real difference; a
// string timestamp on one side and an epoch number on the other is a
// type change a client would feel, so it is never masked.
package noise

import (
	"encoding/json"
	"strings"
	"time"
)

// timeLayouts are the string timestamp formats treated as noise. They
// cover what real APIs emit: RFC 3339 (with or without fractions and
// offsets), the space-separated SQL style, bare dates, and the HTTP
// header date format.
var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02",
	time.RFC1123Z,
	time.RFC1123,
}

// IsTimestampString reports whether s parses as one of the accepted
// timestamp layouts. Plain years ("2026") and version strings
// ("2.4.1") deliberately do not match.
func IsTimestampString(s string) bool {
	if len(s) < 8 || len(s) > 40 {
		return false
	}
	for _, layout := range timeLayouts {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

// Epoch ranges: seconds/millis/micros/nanos between 2001-09-09 and
// 2096-10-02. Everything outside is far more likely to be a count or
// an id than a clock reading.
const (
	epochSecMin = int64(1_000_000_000)
	epochSecMax = int64(4_000_000_000)
)

// IsEpochNumber reports whether n is plausibly a Unix timestamp in
// seconds, milliseconds, microseconds or nanoseconds.
func IsEpochNumber(n json.Number) bool {
	i, err := n.Int64()
	if err != nil {
		// Fractional epoch seconds ("1752314400.123") are common in
		// Python-generated APIs.
		f, ferr := n.Float64()
		if ferr != nil {
			return false
		}
		return f >= float64(epochSecMin) && f < float64(epochSecMax)
	}
	for _, scale := range []int64{1, 1_000, 1_000_000, 1_000_000_000} {
		lo, hi := epochSecMin*scale, epochSecMax*scale
		if lo <= 0 || hi <= 0 { // overflow guard for the nano scale
			continue
		}
		if i >= lo && i < hi {
			return true
		}
	}
	// Nanoseconds overflow the loop above; check the range directly.
	return i >= 1_000_000_000_000_000_000 && i <= 4_000_000_000_000_000_000
}

// IsTimestamp reports whether v (string or json.Number) is
// timestamp-shaped. Digit-only strings in the epoch ranges count,
// because serializers frequently stringify epoch fields.
func IsTimestamp(v any) bool {
	switch t := v.(type) {
	case string:
		if IsTimestampString(t) {
			return true
		}
		if isDigits(t) {
			return IsEpochNumber(json.Number(t))
		}
		return false
	case json.Number:
		return IsEpochNumber(t)
	}
	return false
}

// IDShape names the identifier family a string matched. Two values are
// only mutually noise when their shapes are equal (and, for prefixed
// ids, the prefixes are equal too).
type IDShape string

const (
	ShapeNone     IDShape = ""
	ShapeUUID     IDShape = "uuid"
	ShapeULID     IDShape = "ulid"
	ShapeHex      IDShape = "hex"
	ShapePrefixed IDShape = "prefixed"
)

// IDOf classifies s into an identifier shape, or ShapeNone. For
// ShapePrefixed the second return holds the prefix (e.g. "req" for
// "req_9f8e7d6c5b4a").
func IDOf(s string) (IDShape, string) {
	if isUUID(s) {
		return ShapeUUID, ""
	}
	if isULID(s) {
		return ShapeULID, ""
	}
	if isHexID(s) {
		return ShapeHex, ""
	}
	if p, ok := prefixedID(s); ok {
		return ShapePrefixed, p
	}
	return ShapeNone, ""
}

// SameIDNoise reports whether a and b are both identifier-shaped in a
// mutually compatible way.
func SameIDNoise(a, b string) bool {
	sa, pa := IDOf(a)
	sb, pb := IDOf(b)
	if sa == ShapeNone || sa != sb {
		return false
	}
	if sa == ShapePrefixed {
		return pa == pb
	}
	return true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isHexByte(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// isUUID matches the canonical 8-4-4-4-12 form, any case, any version.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < 36; i++ {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return false
			}
		default:
			if !isHexByte(s[i]) {
				return false
			}
		}
	}
	return true
}

// isULID matches the 26-character Crockford base32 alphabet used by
// ULIDs (the sortable ids emitted by many Go and Node services).
func isULID(s string) bool {
	if len(s) != 26 {
		return false
	}
	hasDigit := false
	for i := 0; i < 26; i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			hasDigit = true
		case c >= 'A' && c <= 'Z':
			if c == 'I' || c == 'L' || c == 'O' || c == 'U' {
				return false
			}
		default:
			return false
		}
	}
	// All-letter 26-char strings are more likely words than ULIDs, and
	// every real ULID carries digits in its timestamp prefix.
	return hasDigit
}

// hexIDLengths are the lengths of common hex identifiers: 64/128-bit
// tokens, Mongo ObjectIds (24), MD5 (32), SHA-1 (40), SHA-256 (64).
var hexIDLengths = map[int]bool{16: true, 20: true, 24: true, 32: true, 40: true, 64: true}

// isHexID matches fixed-width hex identifiers. At least one decimal
// digit is required so that an improbable-but-possible all-letter
// string is not swallowed.
func isHexID(s string) bool {
	if !hexIDLengths[len(s)] {
		return false
	}
	hasDigit := false
	for i := 0; i < len(s); i++ {
		if !isHexByte(s[i]) {
			return false
		}
		if s[i] >= '0' && s[i] <= '9' {
			hasDigit = true
		}
	}
	return hasDigit
}

// prefixedID matches Stripe-style ids: a short lowercase prefix, one
// underscore, then at least 8 alphanumerics with at least one digit
// ("req_9f8e7d6c5b4a", "cus_OqX8g2m1KfT3").
func prefixedID(s string) (string, bool) {
	underscore := strings.IndexByte(s, '_')
	if underscore < 2 || underscore > 8 {
		return "", false
	}
	prefix, body := s[:underscore], s[underscore+1:]
	for i := 0; i < len(prefix); i++ {
		if prefix[i] < 'a' || prefix[i] > 'z' {
			return "", false
		}
	}
	if len(body) < 8 {
		return "", false
	}
	hasDigit := false
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case c >= '0' && c <= '9':
			hasDigit = true
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		default:
			return "", false
		}
	}
	return prefix, hasDigit
}
