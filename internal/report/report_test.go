// Tests for the three renderers. Reports are the product — a diff
// engine nobody can read is worthless — so exact wording of verdicts
// and the JSON schema shape are pinned here.
package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JaydenCJ/twinget/internal/diff"
)

// fixtures used across renderer tests.
func parityResult() diff.RequestResult {
	return diff.RequestResult{
		Method: "GET", Path: "/api/health",
		A: diff.Side{URL: "http://127.0.0.1:8801/api/health", Status: 200, DurationMS: 1.5, BodyBytes: 83},
		B: diff.Side{URL: "http://127.0.0.1:8802/api/health", Status: 200, DurationMS: 1.2, BodyBytes: 81},
		Differences: []diff.Difference{
			{Target: diff.TargetBody, Path: "$.checked_at", Kind: diff.KindValue,
				A: `"2026-07-12T10:00:00.000Z"`, B: `"2026-07-12T10:00:07Z"`,
				Ignored: true, Reason: "timestamp noise"},
		},
	}
}

func diffResult() diff.RequestResult {
	return diff.RequestResult{
		Method: "GET", Path: "/api/users",
		A: diff.Side{URL: "http://127.0.0.1:8801/api/users", Status: 200, DurationMS: 2.0, BodyBytes: 473},
		B: diff.Side{URL: "http://127.0.0.1:8802/api/users", Status: 200, DurationMS: 1.1, BodyBytes: 448},
		Differences: []diff.Difference{
			{Target: diff.TargetBody, Path: "$.users[0].role", Kind: diff.KindValue,
				A: `"admin"`, B: `"administrator"`},
			{Target: diff.TargetBody, Path: "$.users[1].email", Kind: diff.KindMissingB,
				A: `"ben@example.test"`},
			{Target: diff.TargetHeader, Path: "x-request-id", Kind: diff.KindHeader,
				A: "one", B: "two", Ignored: true, Reason: "default header noise"},
		},
	}
}

func meta() Meta {
	return Meta{BaseA: "http://127.0.0.1:8801", BaseB: "http://127.0.0.1:8802"}
}

func TestTextSingleParityVerdict(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, meta(), []diff.RequestResult{parityResult()})
	out := buf.String()
	if !strings.Contains(out, "result: PARITY (1 ignored as noise)") {
		t.Fatalf("missing parity verdict:\n%s", out)
	}
	if strings.Contains(out, "$.checked_at") {
		t.Fatalf("ignored diff should be hidden without --show-ignored:\n%s", out)
	}
}

func TestTextShowIgnoredListsSuppressedDifferences(t *testing.T) {
	var buf bytes.Buffer
	m := meta()
	m.ShowIgnored = true
	Text(&buf, m, []diff.RequestResult{parityResult()})
	out := buf.String()
	if !strings.Contains(out, "$.checked_at") || !strings.Contains(out, "(ignored: timestamp noise)") {
		t.Fatalf("ignored diff should be listed with its reason:\n%s", out)
	}
}

func TestTextSingleDiffListsOnlyEffectiveDifferences(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, meta(), []diff.RequestResult{diffResult()})
	out := buf.String()
	for _, want := range []string{
		"$.users[0].role", `a: "admin"`, `b: "administrator"`,
		"$.users[1].email", "only in a",
		"result: DIFF — 2 differences (1 ignored as noise)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "x-request-id") {
		t.Fatalf("ignored header should not be listed:\n%s", out)
	}
}

func TestTextBatchSummaryAndVerdictLines(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, meta(), []diff.RequestResult{diffResult(), parityResult()})
	out := buf.String()
	if !strings.Contains(out, "DIFF") || !strings.Contains(out, "parity") {
		t.Fatalf("verdict lines missing:\n%s", out)
	}
	if !strings.Contains(out, "2 requests: 1 parity, 1 diff — FAIL") {
		t.Fatalf("summary line missing:\n%s", out)
	}
	// An all-parity batch closes with OK instead.
	buf.Reset()
	Text(&buf, meta(), []diff.RequestResult{parityResult(), parityResult()})
	if !strings.Contains(buf.String(), "— OK") {
		t.Fatalf("expected OK verdict:\n%s", buf.String())
	}
}

func TestJSONEnvelopeShape(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, meta(), []diff.RequestResult{diffResult()}); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if doc["tool"] != "twinget" || doc["schema_version"] != float64(1) {
		t.Fatalf("envelope: %v", doc)
	}
	results := doc["results"].([]any)
	r := results[0].(map[string]any)
	if r["parity"] != false {
		t.Fatalf("parity flag: %v", r["parity"])
	}
	counts := r["counts"].(map[string]any)
	if counts["differences"] != float64(2) || counts["ignored"] != float64(1) {
		t.Fatalf("counts: %v", counts)
	}
}

func TestJSONIncludesIgnoredDifferencesForAudit(t *testing.T) {
	var buf bytes.Buffer
	_ = JSON(&buf, meta(), []diff.RequestResult{parityResult()})
	out := buf.String()
	if !strings.Contains(out, `"ignored": true`) || !strings.Contains(out, "timestamp noise") {
		t.Fatalf("ignored differences must be auditable:\n%s", out)
	}
	// And when there are no differences at all, the field is an empty
	// array, never null — downstream parsers depend on it.
	buf.Reset()
	r := parityResult()
	r.Differences = nil
	_ = JSON(&buf, meta(), []diff.RequestResult{r})
	if !strings.Contains(buf.String(), `"differences": []`) {
		t.Fatalf("nil slice must serialize as []:\n%s", buf.String())
	}
}

func TestMarkdownVerdictTableAndDetailTables(t *testing.T) {
	var buf bytes.Buffer
	Markdown(&buf, meta(), []diff.RequestResult{diffResult(), parityResult()})
	out := buf.String()
	for _, want := range []string{
		"## twinget parity report",
		"| `GET /api/users` | ❌ diff | 2 | 1 |",
		"| `GET /api/health` | ✅ parity | 0 | 1 |",
		"### GET /api/users",
		"`$.users[0].role`",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "### GET /api/health") {
		t.Fatalf("parity request should have no detail section:\n%s", out)
	}
}

func TestMarkdownEscapesPipesInValues(t *testing.T) {
	var buf bytes.Buffer
	r := diffResult()
	r.Differences = []diff.Difference{{
		Target: diff.TargetBody, Path: "$.q", Kind: diff.KindValue,
		A: `"a|b"`, B: `"c"`,
	}}
	Markdown(&buf, meta(), []diff.RequestResult{r})
	if !strings.Contains(buf.String(), `a\|b`) {
		t.Fatalf("pipe not escaped:\n%s", buf.String())
	}
}

func TestSummarizeCounts(t *testing.T) {
	s := Summarize([]diff.RequestResult{parityResult(), diffResult(), parityResult()})
	if s.Requests != 3 || s.Parity != 2 || s.Diff != 1 {
		t.Fatalf("summary: %+v", s)
	}
}
