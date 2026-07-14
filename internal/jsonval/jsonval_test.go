// Tests for JSON parsing, canonical rendering and number equality —
// the primitives the diff engine trusts blindly.
package jsonval

import (
	"encoding/json"
	"testing"
)

func TestParsePreservesBigIntegersExactly(t *testing.T) {
	// 2^53+1 is the classic float64 casualty; json.Number must carry it.
	v, err := Parse([]byte(`{"id": 9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	n := v.(map[string]any)["id"].(json.Number)
	if n.String() != "9007199254740993" {
		t.Fatalf("big integer mangled: %s", n)
	}
}

func TestParseRejectsMalformedInput(t *testing.T) {
	// Two concatenated documents must not silently half-parse.
	for _, body := range []string{`{"a":1} {"b":2}`, "", "   ", "{", `{"a":}`} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Fatalf("Parse(%q) should fail", body)
		}
	}
}

func TestLooksClassifiesBodies(t *testing.T) {
	for _, body := range []string{`{}`, `[]`, `"x"`, `12.5`, `-3`, `true`, `false`, `null`} {
		if !Looks([]byte(body)) {
			t.Fatalf("Looks(%q) = false, want true", body)
		}
	}
	for _, body := range []string{"<html></html>", "hello world", "ok", ""} {
		if Looks([]byte(body)) {
			t.Fatalf("Looks(%q) = true, want false", body)
		}
	}
}

func TestTypeNameCoversEveryJSONType(t *testing.T) {
	cases := map[string]any{
		"null":    nil,
		"boolean": true,
		"number":  json.Number("1"),
		"string":  "s",
		"array":   []any{},
		"object":  map[string]any{},
	}
	for want, v := range cases {
		if got := TypeName(v); got != want {
			t.Fatalf("TypeName(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestCanonicalFormIsSortedAndStable(t *testing.T) {
	v, _ := Parse([]byte(`{"b":1,"a":{"z":true,"y":null}}`))
	got := Canonical(v)
	want := `{"a":{"y":null,"z":true},"b":1}`
	if got != want {
		t.Fatalf("Canonical = %s, want %s", got, want)
	}
	// Two different serializations of the same tree must canonicalize
	// identically — unordered array comparison depends on this.
	v1, _ := Parse([]byte(`{"x": [1, 2], "y": "s"}`))
	v2, _ := Parse([]byte("{\"y\":\"s\",\"x\":[1,2]}"))
	if Canonical(v1) != Canonical(v2) {
		t.Fatal("canonical forms differ for equal trees")
	}
	// SortedKeys is the primitive underneath; pin its order too.
	keys := SortedKeys(map[string]any{"c": 1, "a": 2, "b": 3})
	if keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Fatalf("SortedKeys = %v", keys)
	}
}

func TestSnippetRendering(t *testing.T) {
	// Strings are quoted like JSON, so "2" vs 2 is visually distinct.
	if got := Snippet("admin"); got != `"admin"` {
		t.Fatalf("Snippet = %s", got)
	}
	// Long values are truncated with an ellipsis.
	long := make([]any, 40)
	for i := range long {
		long[i] = json.Number("7")
	}
	s := Snippet(long)
	if n := len([]rune(s)); n > 64 {
		t.Fatalf("snippet too long: %d runes", n)
	}
	if s[len(s)-len("…"):] != "…" {
		t.Fatalf("snippet should end with an ellipsis: %q", s)
	}
}

func TestNumbersEqualAcrossRepresentations(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1", "1.0", true},    // int vs float spelling
		{"1e3", "1000", true}, // exponent vs plain
		{"0.1", "0.2", false},
		{"9007199254740993", "9007199254740992", false}, // beyond float64 precision
		{"9007199254740993", "9007199254740993", true},
	}
	for _, c := range cases {
		if got := NumbersEqual(json.Number(c.a), json.Number(c.b)); got != c.want {
			t.Fatalf("NumbersEqual(%s, %s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
