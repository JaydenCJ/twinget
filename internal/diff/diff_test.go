// Tests for the structural diff engine: status, headers, JSON bodies,
// noise filters, unordered arrays, and the non-JSON fallbacks.
package diff

import (
	"strings"
	"testing"

	"github.com/JaydenCJ/twinget/internal/jsonpath"
)

// bodyDiff runs Bodies with options and returns all differences.
func bodyDiff(t *testing.T, a, b string, opts Options) []Difference {
	t.Helper()
	return Bodies([]byte(a), []byte(b), opts)
}

// effective filters out ignored differences.
func effective(diffs []Difference) []Difference {
	var out []Difference
	for _, d := range diffs {
		if !d.Ignored {
			out = append(out, d)
		}
	}
	return out
}

func mustPatterns(t *testing.T, raws ...string) []jsonpath.Pattern {
	t.Helper()
	var out []jsonpath.Pattern
	for _, raw := range raws {
		p, err := jsonpath.ParsePattern(raw)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func TestStatusComparison(t *testing.T) {
	if diffs := Status(200, 200); len(diffs) != 0 {
		t.Fatalf("equal statuses: got %d diffs", len(diffs))
	}
	diffs := Status(200, 404)
	if len(diffs) != 1 || diffs[0].Kind != KindStatus || diffs[0].A != "200" || diffs[0].B != "404" {
		t.Fatalf("unexpected: %+v", diffs)
	}
}

func TestSemanticallyEqualBodiesAreParity(t *testing.T) {
	// Key order never matters…
	if diffs := bodyDiff(t, `{"a":1,"b":[true,null]}`, `{"b":[true,null],"a":1}`, Options{}); len(diffs) != 0 {
		t.Fatalf("key order: %+v", diffs)
	}
	// …and neither does number spelling (1.0 vs 1, 1e3 vs 1000).
	if diffs := bodyDiff(t, `{"n":1.0,"m":1e3}`, `{"n":1,"m":1000}`, Options{}); len(diffs) != 0 {
		t.Fatalf("number spelling: %+v", diffs)
	}
}

func TestValueChangeIsLocatedByPath(t *testing.T) {
	diffs := bodyDiff(t,
		`{"users":[{"role":"admin"}]}`,
		`{"users":[{"role":"administrator"}]}`, Options{})
	if len(diffs) != 1 {
		t.Fatalf("got %d diffs", len(diffs))
	}
	d := diffs[0]
	if d.Path != "$.users[0].role" || d.Kind != KindValue {
		t.Fatalf("unexpected: %+v", d)
	}
	if d.A != `"admin"` || d.B != `"administrator"` {
		t.Fatalf("snippets: %q vs %q", d.A, d.B)
	}
}

func TestTypeChangeIsNamedNotJustDifferent(t *testing.T) {
	// number 2 vs string "2" is the classic silent-rewrite regression.
	diffs := bodyDiff(t, `{"total":2}`, `{"total":"2"}`, Options{})
	if len(diffs) != 1 || diffs[0].Kind != KindType {
		t.Fatalf("unexpected: %+v", diffs)
	}
	if !strings.Contains(diffs[0].A, "number") || !strings.Contains(diffs[0].B, "string") {
		t.Fatalf("type names missing: %+v", diffs[0])
	}
}

func TestMissingKeysAreDirectional(t *testing.T) {
	diffs := bodyDiff(t, `{"email":"x@example.test"}`, `{}`, Options{})
	if len(diffs) != 1 || diffs[0].Kind != KindMissingB {
		t.Fatalf("unexpected: %+v", diffs)
	}
	diffs = bodyDiff(t, `{}`, `{"email":"x@example.test"}`, Options{})
	if len(diffs) != 1 || diffs[0].Kind != KindMissingA {
		t.Fatalf("unexpected: %+v", diffs)
	}
}

func TestArrayLengthAndSurplusElements(t *testing.T) {
	diffs := bodyDiff(t, `[1,2,3]`, `[1,2]`, Options{})
	// One length diff at $ and one "only in a" for element [2].
	if len(diffs) != 2 || diffs[0].Kind != KindLength || diffs[1].Kind != KindMissingB {
		t.Fatalf("unexpected: %+v", diffs)
	}
	if diffs[1].Path != "$[2]" {
		t.Fatalf("surplus element path = %q", diffs[1].Path)
	}
}

func TestPathsFromRootScalarToDeepNesting(t *testing.T) {
	// A root-level scalar diff reports at $ itself.
	diffs := bodyDiff(t, `true`, `false`, Options{})
	if len(diffs) != 1 || diffs[0].Path != "$" || diffs[0].Kind != KindValue {
		t.Fatalf("root scalar: %+v", diffs)
	}
	// A deep difference carries its full path.
	diffs = bodyDiff(t,
		`{"data":{"items":[{"tags":["a","b"]}]}}`,
		`{"data":{"items":[{"tags":["a","c"]}]}}`, Options{})
	if len(diffs) != 1 || diffs[0].Path != "$.data.items[0].tags[1]" {
		t.Fatalf("nested: %+v", diffs)
	}
}

func TestIgnorePathSilencesSubtreeButKeepsCount(t *testing.T) {
	opts := Options{IgnorePaths: mustPatterns(t, "$.meta")}
	diffs := bodyDiff(t,
		`{"meta":{"ts":1,"trace":{"id":2}},"v":1}`,
		`{"meta":{"ts":9,"trace":{"id":8}},"v":2}`, opts)
	eff := effective(diffs)
	if len(eff) != 1 || eff[0].Path != "$.v" {
		t.Fatalf("effective = %+v", eff)
	}
	if len(diffs) != 3 {
		t.Fatalf("ignored differences must still be recorded, got %d", len(diffs))
	}
	for _, d := range diffs {
		if d.Ignored && !strings.Contains(d.Reason, "$.meta") {
			t.Fatalf("reason should quote the pattern: %+v", d)
		}
	}
}

func TestIgnorePathAlsoCoversMissingKeys(t *testing.T) {
	// A key that exists on one side only, under an ignored path, is noise.
	opts := Options{IgnorePaths: mustPatterns(t, "$.debug.**")}
	diffs := bodyDiff(t, `{"debug":{"pid":1}}`, `{"debug":{}}`, opts)
	if len(effective(diffs)) != 0 {
		t.Fatalf("got %+v", diffs)
	}
}

func TestIgnoreTimestampsNeutralizesOnlyTimestamps(t *testing.T) {
	opts := Options{IgnoreTimestamps: true}
	diffs := bodyDiff(t,
		`{"at":"2026-07-12T10:00:00.000Z","epoch":1752314400}`,
		`{"at":"2026-07-12T10:00:07Z","epoch":1752314407}`, opts)
	if len(effective(diffs)) != 0 {
		t.Fatalf("got %+v", effective(diffs))
	}
	if len(diffs) != 2 {
		t.Fatalf("both should be recorded as ignored, got %d", len(diffs))
	}
	// Version strings and other non-timestamps stay real differences.
	diffs = bodyDiff(t, `{"v":"1.2.3"}`, `{"v":"1.2.4"}`, opts)
	if len(effective(diffs)) != 1 {
		t.Fatalf("version strings must not be masked: %+v", diffs)
	}
}

func TestIgnoreTimestampsNeverMasksTypeChanges(t *testing.T) {
	// String timestamp on one side, epoch number on the other: clients
	// would break, so this must stay a real difference.
	opts := Options{IgnoreTimestamps: true}
	diffs := bodyDiff(t, `{"at":"2026-07-12T10:00:00Z"}`, `{"at":1752314400}`, opts)
	eff := effective(diffs)
	if len(eff) != 1 || eff[0].Kind != KindType {
		t.Fatalf("unexpected: %+v", diffs)
	}
}

func TestIgnoreIDsNeutralizesSameShapeOnly(t *testing.T) {
	opts := Options{IgnoreIDs: true}
	diffs := bodyDiff(t,
		`{"rid":"7f9c24e5-3b1a-4d2e-9c8f-1a2b3c4d5e6f","name":"alpha"}`,
		`{"rid":"0d9e8f7a-6b5c-4d3e-2f1a-0b9c8d7e6f5a","name":"beta"}`, opts)
	eff := effective(diffs)
	if len(eff) != 1 || eff[0].Path != "$.name" {
		t.Fatalf("unexpected: %+v", eff)
	}
}

func TestUnorderedArrayMultisetSemantics(t *testing.T) {
	opts := Options{UnorderedPaths: mustPatterns(t, "$.tags")}
	// Same elements, any order: parity.
	diffs := bodyDiff(t, `{"tags":["a","b","c"]}`, `{"tags":["c","a","b"]}`, opts)
	if len(diffs) != 0 {
		t.Fatalf("reordered array should be parity: %+v", diffs)
	}
	// Duplicate counts still matter: ["a","b","b"] vs ["b","a"].
	diffs = bodyDiff(t, `{"tags":["a","b","b"]}`, `{"tags":["b","a"]}`, opts)
	if len(diffs) != 1 || diffs[0].Kind != KindMissingB || diffs[0].A != `"b"` {
		t.Fatalf("duplicate count must matter: %+v", diffs)
	}
}

func TestUnorderedDoesNotLeakIntoNestedArrays(t *testing.T) {
	// Only the flagged array is order-free; arrays inside its elements
	// keep strict ordering.
	opts := Options{UnorderedPaths: mustPatterns(t, "$.items")}
	diffs := bodyDiff(t,
		`{"items":[{"seq":[1,2]}]}`,
		`{"items":[{"seq":[2,1]}]}`, opts)
	if len(diffs) == 0 {
		t.Fatal("nested array reorder should still be a difference")
	}
}

func TestHeaderDiffIsCaseInsensitiveAndSorted(t *testing.T) {
	a := map[string][]string{"Content-Type": {"application/json; charset=utf-8"}, "Allow": {"GET"}}
	b := map[string][]string{"content-type": {"application/json"}, "allow": {"GET"}}
	diffs := Headers(a, b, Options{})
	if len(diffs) != 1 || diffs[0].Path != "content-type" {
		t.Fatalf("unexpected: %+v", diffs)
	}
}

func TestHeaderNoiseDefaultsStrictModeAndFlag(t *testing.T) {
	// Default: volatile headers are ignored but recorded.
	a := map[string][]string{"Date": {"Sat, 12 Jul 2026 10:00:00 GMT"}, "X-Request-Id": {"one"}}
	b := map[string][]string{"Date": {"Sat, 12 Jul 2026 10:00:07 GMT"}, "X-Request-Id": {"two"}}
	diffs := Headers(a, b, Options{})
	if len(diffs) != 2 || len(effective(diffs)) != 0 {
		t.Fatalf("default noise: %+v", diffs)
	}
	// --strict-headers disables the built-in list.
	a = map[string][]string{"Server": {"legacy-node/14.21"}}
	b = map[string][]string{"Server": {"go-rewrite/2.0"}}
	diffs = Headers(a, b, Options{StrictHeaders: true})
	if len(effective(diffs)) != 1 {
		t.Fatalf("strict mode should surface Server: %+v", diffs)
	}
	// --ignore-header still applies even in strict mode.
	a = map[string][]string{"Content-Type": {"a"}}
	b = map[string][]string{"Content-Type": {"b"}}
	diffs = Headers(a, b, Options{StrictHeaders: true, IgnoreHeaders: []string{"Content-Type"}})
	if len(diffs) != 1 || !diffs[0].Ignored {
		t.Fatalf("ignore-header in strict mode: %+v", diffs)
	}
}

func TestHeaderMissingSidesAndMultiValueJoin(t *testing.T) {
	a := map[string][]string{"Location": {"/v1/thing"}}
	diffs := Headers(a, map[string][]string{}, Options{})
	if len(diffs) != 1 || diffs[0].Kind != KindMissingB {
		t.Fatalf("missing side: %+v", diffs)
	}
	// Multi-valued headers join in received order — order is contract.
	a = map[string][]string{"Vary": {"Accept", "Origin"}}
	b := map[string][]string{"Vary": {"Origin", "Accept"}}
	diffs = Headers(a, b, Options{})
	if len(diffs) != 1 || diffs[0].A != "Accept, Origin" || diffs[0].B != "Origin, Accept" {
		t.Fatalf("multi-value: %+v", diffs)
	}
}

func TestNonJSONBodiesFallBackToTextComparison(t *testing.T) {
	diffs := Bodies([]byte("hello\nworld\n"), []byte("hello\nplanet\n"), Options{})
	if len(diffs) != 1 || diffs[0].Kind != KindText {
		t.Fatalf("unexpected: %+v", diffs)
	}
	if diffs[0].Path != "line 2" {
		t.Fatalf("first divergence should be located: %+v", diffs[0])
	}
	if diffs := Bodies([]byte("same"), []byte("same"), Options{}); len(diffs) != 0 {
		t.Fatalf("equal text bodies: %+v", diffs)
	}
}

func TestJSONVersusNonJSONIsAFormatDifference(t *testing.T) {
	diffs := Bodies([]byte(`{"ok":true}`), []byte("<html>oops</html>"), Options{})
	if len(diffs) != 1 || diffs[0].Kind != KindBodyFormat {
		t.Fatalf("unexpected: %+v", diffs)
	}
	if !strings.Contains(diffs[0].A, "JSON") || !strings.Contains(diffs[0].B, "non-JSON") {
		t.Fatalf("descriptions: %+v", diffs[0])
	}
}

func TestDifferencesAreDeterministicallyOrdered(t *testing.T) {
	// Repeated runs over maps with many keys must produce the same order.
	a := `{"z":1,"y":2,"x":3,"w":4,"v":5}`
	b := `{"z":9,"y":8,"x":7,"w":6,"v":5}`
	first := bodyDiff(t, a, b, Options{})
	for i := 0; i < 10; i++ {
		again := bodyDiff(t, a, b, Options{})
		for j := range first {
			if first[j] != again[j] {
				t.Fatalf("run %d: order changed at %d", i, j)
			}
		}
	}
}

func TestRequestResultCountsAndParity(t *testing.T) {
	r := RequestResult{Differences: []Difference{
		{Ignored: true}, {Ignored: false}, {Ignored: true},
	}}
	eff, ign := r.Counts()
	if eff != 1 || ign != 2 || r.Parity() {
		t.Fatalf("Counts = %d/%d, Parity = %v", eff, ign, r.Parity())
	}
	all := RequestResult{Differences: []Difference{{Ignored: true}}}
	if !all.Parity() {
		t.Fatal("only-ignored differences should be parity")
	}
}
