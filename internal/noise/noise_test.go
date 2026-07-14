// Tests for the noise classifiers. False positives here would mask
// real regressions, so the negative cases matter more than the
// positive ones.
package noise

import (
	"encoding/json"
	"testing"
)

func TestTimestampStringClassification(t *testing.T) {
	positives := []string{
		"2026-07-12T10:00:00Z",             // RFC 3339
		"2026-07-12T10:00:00.123Z",         // fractional seconds
		"2026-07-12T10:00:00+09:00",        // zone offset
		"2026-07-12T10:00:00",              // no zone (common in Java APIs)
		"2026-07-12 10:00:00",              // SQL style
		"2026-07-12",                       // bare date
		"Sat, 12 Jul 2026 10:00:00 GMT",    // RFC 1123 (HTTP dates)
		"2026-07-12 10:00:00.123456+02:00", // SQL style with zone
	}
	for _, s := range positives {
		if !IsTimestampString(s) {
			t.Fatalf("IsTimestampString(%q) = false, want true", s)
		}
	}
	negatives := []string{
		"2026",       // a year is more likely a count or a name
		"2.4.1",      // version string
		"12:30:00",   // time without a date: too ambiguous
		"not a date", //
		"2026-13-40", // impossible month/day
		"",           //
		"20260712",   // compact digits: could be anything
	}
	for _, s := range negatives {
		if IsTimestampString(s) {
			t.Fatalf("IsTimestampString(%q) = true, want false", s)
		}
	}
}

func TestEpochNumberRanges(t *testing.T) {
	positives := []string{
		"1752314400",          // seconds (2026)
		"1752314400.123",      // fractional seconds
		"1752314400123",       // milliseconds
		"1752314400123456",    // microseconds
		"1752314400123456789", // nanoseconds
	}
	for _, s := range positives {
		if !IsEpochNumber(json.Number(s)) {
			t.Fatalf("IsEpochNumber(%s) = false, want true", s)
		}
	}
	negatives := []string{
		"42",          // small count
		"999999999",   // just below the seconds floor (2001)
		"4000000001",  // just above the seconds ceiling — still far from millis
		"-1752314400", // negative epochs are not emitted by real APIs
		"0",
	}
	for _, s := range negatives {
		if IsEpochNumber(json.Number(s)) {
			t.Fatalf("IsEpochNumber(%s) = true, want false", s)
		}
	}
}

func TestIsTimestampAcceptsDigitStringsInEpochRange(t *testing.T) {
	// Serializers frequently stringify epoch fields.
	if !IsTimestamp("1752314400") {
		t.Fatal("digit string in epoch range should be timestamp-shaped")
	}
	if IsTimestamp("123") {
		t.Fatal("small digit string must not be timestamp-shaped")
	}
	if IsTimestamp(true) {
		t.Fatal("booleans are never timestamps")
	}
}

func TestUUIDShapeClassification(t *testing.T) {
	for _, s := range []string{
		"7f9c24e5-3b1a-4d2e-9c8f-1a2b3c4d5e6f",
		"7F9C24E5-3B1A-4D2E-9C8F-1A2B3C4D5E6F", // uppercase is valid too
	} {
		if shape, _ := IDOf(s); shape != ShapeUUID {
			t.Fatalf("IDOf(%q) = %q, want uuid", s, shape)
		}
	}
	for _, s := range []string{
		"7f9c24e5-3b1a-4d2e-9c8f-1a2b3c4d5e6",  // one char short
		"7f9c24e5-3b1a-4d2e-9c8f-1a2b3c4d5e6g", // 'g' is not hex
		"7f9c24e53b1a4d2e9c8f1a2b3c4d5e6f00aa", // no dashes and wrong length
	} {
		if shape, _ := IDOf(s); shape == ShapeUUID {
			t.Fatalf("%q should not classify as uuid", s)
		}
	}
}

func TestULIDShapeClassification(t *testing.T) {
	if shape, _ := IDOf("01J2ZK9F8Q4R7T2V5X8B3N6M9C"); shape != ShapeULID {
		t.Fatalf("shape = %q, want ulid", shape)
	}
	// 26 uppercase letters that spell words must not be swallowed:
	// ULIDs always contain digits, and I/L/O/U are outside the alphabet.
	for _, s := range []string{
		"ABCDEFGHJKMNPQRSTVWXYZABCD", // no digits
		"01J2ZK9F8Q4R7T2V5X8B3N6M9I", // contains I
		"01j2zk9f8q4r7t2v5x8b3n6m9c", // lowercase is not ULID
	} {
		if shape, _ := IDOf(s); shape == ShapeULID {
			t.Fatalf("%q should not classify as ulid", s)
		}
	}
}

func TestHexIDShapeClassification(t *testing.T) {
	for _, s := range []string{
		"507f1f77bcf86cd799439011",         // Mongo ObjectId (24)
		"9f8e7d6c5b4a3f2e",                 // 64-bit token (16)
		"d41d8cd98f00b204e9800998ecf8427e", // MD5 (32)
	} {
		if shape, _ := IDOf(s); shape != ShapeHex {
			t.Fatalf("IDOf(%q) = %q, want hex", s, shape)
		}
	}
	for _, s := range []string{
		"9f8e7d6c5b4a3f2",  // 15 chars: not a known id width
		"9f8e7d6c5b4a3f2z", // valid width but 'z' is not hex
		"abcdefabcdefabcd", // 16 hex chars but no digit
	} {
		if shape, _ := IDOf(s); shape == ShapeHex {
			t.Fatalf("%q should not classify as hex id", s)
		}
	}
}

func TestPrefixedIDClassification(t *testing.T) {
	shape, prefix := IDOf("req_9f8e7d6c5b4a")
	if shape != ShapePrefixed || prefix != "req" {
		t.Fatalf("IDOf = %q/%q, want prefixed/req", shape, prefix)
	}
	// Ordinary snake_case words must never be treated as ids.
	for _, s := range []string{"created_at", "user_name", "order_total"} {
		if shape, _ := IDOf(s); shape != ShapeNone {
			t.Fatalf("IDOf(%q) = %q, want none", s, shape)
		}
	}
}

func TestSameIDNoiseRequiresMatchingShapes(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"7f9c24e5-3b1a-4d2e-9c8f-1a2b3c4d5e6f", "0d9e8f7a-6b5c-4d3e-2f1a-0b9c8d7e6f5a", true}, // uuid vs uuid
		{"req_9f8e7d6c5b4a", "req_1a2b3c4d5e6f", true},                                         // same prefix
		{"req_9f8e7d6c5b4a", "cus_1a2b3c4d5e6f", false},                                        // different prefix: a real change
		{"7f9c24e5-3b1a-4d2e-9c8f-1a2b3c4d5e6f", "507f1f77bcf86cd799439011", false},            // uuid vs hex
		{"7f9c24e5-3b1a-4d2e-9c8f-1a2b3c4d5e6f", "banana", false},                              // id vs word
	}
	for _, c := range cases {
		if got := SameIDNoise(c.a, c.b); got != c.want {
			t.Fatalf("SameIDNoise(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestDefaultHeaderNoiseList(t *testing.T) {
	// Volatile headers are ignored regardless of case…
	for _, h := range []string{"Date", "date", "X-Request-Id", "SERVER", "Set-Cookie", "ETag"} {
		if !DefaultIgnoredHeader(h) {
			t.Fatalf("DefaultIgnoredHeader(%q) = false, want true", h)
		}
	}
	// …but headers that carry API contract are always compared.
	for _, h := range []string{"Content-Type", "Cache-Control", "Location", "Allow", "Vary"} {
		if DefaultIgnoredHeader(h) {
			t.Fatalf("DefaultIgnoredHeader(%q) = true, want false", h)
		}
	}
	// docs/noise-filters.md documents the list as exactly 21 headers;
	// growing it without updating the docs is a bug.
	if n := DefaultIgnoredHeaderCount(); n != 21 {
		t.Fatalf("default header noise list has %d entries, docs say 21", n)
	}
}
