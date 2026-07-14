// Tests for batch-file and header-flag parsing. Batch files are
// hand-written by users, so error messages and line numbers are part
// of the contract.
package request

import (
	"strings"
	"testing"
)

func parse(t *testing.T, content string) []Spec {
	t.Helper()
	specs, err := ParseBatch(strings.NewReader(content))
	if err != nil {
		t.Fatalf("ParseBatch: %v", err)
	}
	return specs
}

func TestPlainLineForms(t *testing.T) {
	// "METHOD /path", bare "/path" (implied GET), lowercase methods.
	specs := parse(t, "GET /api/users\n/api/health\npost /api/orders\n")
	if len(specs) != 3 {
		t.Fatalf("got %d specs", len(specs))
	}
	if specs[0].Method != "GET" || specs[0].Path != "/api/users" {
		t.Fatalf("unexpected: %+v", specs[0])
	}
	if specs[1].Method != "GET" || specs[1].Path != "/api/health" {
		t.Fatalf("bare path should imply GET: %+v", specs[1])
	}
	if specs[2].Method != "POST" {
		t.Fatalf("method should be uppercased: %+v", specs[2])
	}
}

func TestCommentsAndBlankLinesAreSkipped(t *testing.T) {
	specs := parse(t, "# smoke set\n\nGET /a\n   \n# tail comment\n/b\n")
	if len(specs) != 2 || specs[0].Path != "/a" || specs[1].Path != "/b" {
		t.Fatalf("unexpected: %+v", specs)
	}
}

func TestQueryStringsSurviveParsing(t *testing.T) {
	specs := parse(t, "GET /api/users?limit=1&sort=name\n")
	if specs[0].Path != "/api/users?limit=1&sort=name" {
		t.Fatalf("path = %q", specs[0].Path)
	}
}

func TestJSONLineWithHeadersAndJSONBody(t *testing.T) {
	specs := parse(t, `{"method":"POST","path":"/api/orders","headers":{"X-Env":"staging"},"body":{"sku":"TWG-1"}}`+"\n")
	s := specs[0]
	if s.Method != "POST" || s.Path != "/api/orders" {
		t.Fatalf("unexpected: %+v", s)
	}
	if string(s.Body) != `{"sku":"TWG-1"}` {
		t.Fatalf("body = %s", s.Body)
	}
	// A JSON body without explicit Content-Type gets one.
	found := false
	for _, h := range s.Headers {
		if h[0] == "Content-Type" && h[1] == "application/json" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing implied Content-Type: %+v", s.Headers)
	}
	// Method is optional and defaults to GET.
	specs = parse(t, `{"path":"/x"}`+"\n")
	if specs[0].Method != "GET" {
		t.Fatalf("method = %q", specs[0].Method)
	}
}

func TestJSONLineStringBodyIsSentVerbatim(t *testing.T) {
	specs := parse(t, `{"method":"POST","path":"/p","body":"raw text"}`+"\n")
	if string(specs[0].Body) != "raw text" {
		t.Fatalf("body = %q", specs[0].Body)
	}
	for _, h := range specs[0].Headers {
		if h[0] == "Content-Type" {
			t.Fatal("string bodies must not imply a Content-Type")
		}
	}
}

func TestErrorsCarryLineNumbers(t *testing.T) {
	_, err := ParseBatch(strings.NewReader("GET /ok\nBOGUS /nope\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v", err)
	}
}

func TestRejectsPathWithoutLeadingSlash(t *testing.T) {
	for _, line := range []string{"GET api/users\n", "api/users\n", `{"path":"api/users"}` + "\n"} {
		if _, err := ParseBatch(strings.NewReader(line)); err == nil {
			t.Fatalf("line %q should fail", line)
		}
	}
}

func TestRejectsUnknownJSONFields(t *testing.T) {
	// Typos like "mehtod" must not be silently dropped.
	_, err := ParseBatch(strings.NewReader(`{"path":"/x","mehtod":"POST"}` + "\n"))
	if err == nil {
		t.Fatal("unknown field should fail")
	}
}

func TestRejectsEmptyBatch(t *testing.T) {
	_, err := ParseBatch(strings.NewReader("# only comments\n\n"))
	if err == nil || !strings.Contains(err.Error(), "no requests") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseHeaderFlag(t *testing.T) {
	name, value, err := ParseHeaderFlag("Authorization: Bearer tok")
	if err != nil || name != "Authorization" || value != "Bearer tok" {
		t.Fatalf("got %q/%q/%v", name, value, err)
	}
	for _, raw := range []string{"NoColon", ": empty-name", ""} {
		if _, _, err := ParseHeaderFlag(raw); err == nil {
			t.Fatalf("ParseHeaderFlag(%q) should fail", raw)
		}
	}
}
