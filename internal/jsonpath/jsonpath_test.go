// Tests for path rendering and pattern matching — the syntax every
// --ignore and --unordered flag flows through, so edge cases here are
// user-visible behavior, not implementation detail.
package jsonpath

import "testing"

func segs(items ...any) []Seg {
	out := make([]Seg, 0, len(items))
	for _, it := range items {
		switch v := it.(type) {
		case string:
			out = append(out, Key(v))
		case int:
			out = append(out, Index(v))
		}
	}
	return out
}

func TestRenderPaths(t *testing.T) {
	cases := []struct {
		segs []Seg
		want string
	}{
		{nil, "$"}, // the root itself
		{segs("users", 2, "name"), "$.users[2].name"},
		// Keys with dots or spaces must be bracket-quoted or the path
		// would be ambiguous when copied back into --ignore.
		{segs("a.b", "full name"), `$["a.b"]["full name"]`},
	}
	for _, c := range cases {
		if got := Render(c.segs); got != c.want {
			t.Fatalf("Render = %q, want %q", got, c.want)
		}
	}
}

func TestPatternLiteralKeysAndIndexes(t *testing.T) {
	p := mustParse(t, "$.meta.request_id")
	if !p.Matches(segs("meta", "request_id")) {
		t.Fatal("expected literal key match")
	}
	if p.Matches(segs("meta", "other")) {
		t.Fatal("unexpected match on different key")
	}
	q := mustParse(t, "$.items[2]")
	if !q.Matches(segs("items", 2)) || q.Matches(segs("items", 3)) {
		t.Fatal("[2] must match exactly index 2")
	}
}

func TestPatternPrefixSemanticsIgnoreSubtree(t *testing.T) {
	// --ignore $.meta must silence everything below $.meta.
	p := mustParse(t, "$.meta")
	if !p.Matches(segs("meta", "deep", 3, "leaf")) {
		t.Fatal("prefix match should cover the whole subtree")
	}
	if p.Matches(segs("metadata")) {
		t.Fatal("`meta` must not match the distinct key `metadata`")
	}
}

func TestPatternSingleSegmentWildcards(t *testing.T) {
	// [*] matches any index — and only indexes.
	p := mustParse(t, "$.users[*].id")
	if !p.Matches(segs("users", 0, "id")) || !p.Matches(segs("users", 41, "id")) {
		t.Fatal("[*] should match any index")
	}
	if p.Matches(segs("users", "id")) {
		t.Fatal("[*] must not match a key segment")
	}
	// * matches exactly one segment of either kind.
	q := mustParse(t, "$.*.id")
	if !q.Matches(segs("users", "id")) || !q.Matches(segs(7, "id")) {
		t.Fatal("* should match one key or index segment")
	}
	if q.Matches(segs("id")) {
		t.Fatal("* must consume exactly one segment")
	}
}

func TestPatternDeepWildcardMatchesAnyDepth(t *testing.T) {
	p := mustParse(t, "$.**.request_id")
	cases := [][]Seg{
		segs("request_id"), // zero segments consumed
		segs("meta", "request_id"),
		segs("data", 3, "meta", "request_id"),
	}
	for _, c := range cases {
		if !p.Matches(c) {
			t.Fatalf("expected ** to match %q", Render(c))
		}
	}
	if p.Matches(segs("meta", "trace_id")) {
		t.Fatal("** still requires the trailing key to match")
	}
	// $..request_id is common JSONPath muscle memory; it must behave
	// like $.**.request_id.
	sugar := mustParse(t, "$..request_id")
	if !sugar.Matches(segs("deep", "nest", "request_id")) {
		t.Fatal("`..` sugar should deep-match")
	}
}

func TestPatternSyntaxVariants(t *testing.T) {
	// Bracket-quoted keys reach keys containing dots.
	p := mustParse(t, `$["a.b"].c`)
	if !p.Matches(segs("a.b", "c")) {
		t.Fatal("quoted key should match key containing a dot")
	}
	// The leading $ is optional.
	q := mustParse(t, "users[*].id")
	if !q.Matches(segs("users", 1, "id")) {
		t.Fatal("bare pattern (no $) should work")
	}
}

func TestPatternExactModeRejectsTrailingSegments(t *testing.T) {
	// --unordered $.items applies to that array only, not to arrays
	// nested inside its elements.
	p := mustParse(t, "$.items")
	if !p.MatchesExact(segs("items")) {
		t.Fatal("exact match on the array path itself")
	}
	if p.MatchesExact(segs("items", 0, "tags")) {
		t.Fatal("exact mode must not match descendants")
	}
}

func TestPatternParseErrors(t *testing.T) {
	for _, raw := range []string{"", "$", "$.a[", "$.a[x]", "$.a[-1]", `$.a["unterminated]`} {
		if _, err := ParsePattern(raw); err == nil {
			t.Fatalf("ParsePattern(%q) should fail", raw)
		}
	}
}

func TestAnyMatchesReturnsFirstHit(t *testing.T) {
	patterns := []Pattern{
		mustParse(t, "$.aaa"),
		mustParse(t, "$.meta.**"),
	}
	p, ok := AnyMatches(patterns, segs("meta", "ts"))
	if !ok || p.String() != "$.meta.**" {
		t.Fatalf("AnyMatches = %q, %v", p.String(), ok)
	}
	if _, ok := AnyMatches(patterns, segs("other")); ok {
		t.Fatal("no pattern should match $.other")
	}
}

func mustParse(t *testing.T, raw string) Pattern {
	t.Helper()
	p, err := ParsePattern(raw)
	if err != nil {
		t.Fatalf("ParsePattern(%q): %v", raw, err)
	}
	return p
}
