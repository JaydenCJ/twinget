// Package jsonval parses response bodies into a deterministic value
// tree and renders values back into the short snippets shown in diff
// reports. Numbers are kept as json.Number so 9007199254740993 and
// 0.1 survive the round trip without float damage.
package jsonval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Parse decodes body into a value tree of map[string]any, []any,
// json.Number, string, bool and nil. It fails on trailing garbage so
// "{}{}" and "1 2" are not silently half-parsed.
func Parse(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	// Reject trailing non-whitespace after the first value.
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("trailing data after JSON value")
	}
	return v, nil
}

// Looks reports whether body parses as a JSON document. The diff
// engine uses it to decide between the structural comparison and the
// plain-text fallback, deliberately ignoring the Content-Type header
// (backends lie about it more often than they emit invalid JSON).
func Looks(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}
	switch trimmed[0] {
	case '{', '[', '"', 't', 'f', 'n', '-',
		'0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		_, err := Parse(body)
		return err == nil
	}
	return false
}

// TypeName names the JSON type of v for "type changed" reports.
func TypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case json.Number:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// SortedKeys returns the keys of m in lexicographic order so that every
// walk over an object — and therefore every report — is deterministic.
func SortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// snippetMax bounds how much of a value a report line quotes.
const snippetMax = 64

// Snippet renders v as a compact single-line JSON fragment, truncated
// with an ellipsis when longer than snippetMax runes.
func Snippet(v any) string {
	s := Canonical(v)
	runes := []rune(s)
	if len(runes) <= snippetMax {
		return s
	}
	return string(runes[:snippetMax-1]) + "…"
}

// Canonical renders v as compact JSON with object keys sorted, so equal
// trees always render identically. It is the basis for unordered-array
// comparison and for snippet output.
func Canonical(v any) string {
	var b strings.Builder
	writeCanonical(&b, v)
	return b.String()
}

func writeCanonical(b *strings.Builder, v any) {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case json.Number:
		b.WriteString(t.String())
	case string:
		enc, _ := json.Marshal(t)
		b.Write(enc)
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			writeCanonical(b, e)
		}
		b.WriteByte(']')
	case map[string]any:
		b.WriteByte('{')
		for i, k := range SortedKeys(t) {
			if i > 0 {
				b.WriteByte(',')
			}
			enc, _ := json.Marshal(k)
			b.Write(enc)
			b.WriteByte(':')
			writeCanonical(b, t[k])
		}
		b.WriteByte('}')
	default:
		fmt.Fprintf(b, "%v", t)
	}
}

// NumbersEqual compares two json.Numbers by numeric value, so 1 vs 1.0
// and 1e3 vs 1000 are equal even though their source text differs.
// Integers up to 64 bits are compared exactly; everything else falls
// back to float64.
func NumbersEqual(a, b json.Number) bool {
	if a.String() == b.String() {
		return true
	}
	ai, errA := a.Int64()
	bi, errB := b.Int64()
	if errA == nil && errB == nil {
		return ai == bi
	}
	af, errA := a.Float64()
	bf, errB := b.Float64()
	if errA != nil || errB != nil {
		return false
	}
	return af == bf
}
