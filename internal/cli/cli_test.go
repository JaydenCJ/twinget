// In-process integration tests: cli.Run against twin httptest servers
// on 127.0.0.1. These pin the exit-code contract (0 parity, 1 diff,
// 2 usage, 3 transport) that CI scripts and pre-deploy gates rely on.
package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jsonHandler serves a fixed JSON document with optional headers. The
// automatic Date header is suppressed so that a test never straddles a
// second boundary and changes the ignored-noise count.
func jsonHandler(status int, body string, headers map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header()["Date"] = nil
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// twin starts two servers and returns their base URLs.
func twin(t *testing.T, a, b http.Handler) (string, string) {
	t.Helper()
	sa := httptest.NewServer(a)
	sb := httptest.NewServer(b)
	t.Cleanup(sa.Close)
	t.Cleanup(sb.Close)
	return sa.URL, sb.URL
}

// run executes the CLI in-process and returns exit code and streams.
func run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestVersionAndHelp(t *testing.T) {
	for _, argv := range [][]string{{"version"}, {"--version"}} {
		code, out, _ := run(argv...)
		if code != ExitParity || out != "twinget 0.1.0\n" {
			t.Fatalf("%v -> code=%d out=%q", argv, code, out)
		}
	}
	code, out, _ := run("--help")
	if code != ExitParity || !strings.Contains(out, "Usage:") {
		t.Fatalf("--help -> code=%d out=%q", code, out)
	}
}

func TestDiffParityExitsZero(t *testing.T) {
	h := jsonHandler(200, `{"ok":true}`, nil)
	a, b := twin(t, h, h)
	code, out, _ := run("diff", "--a", a, "--b", b, "/x")
	if code != ExitParity {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "result: PARITY") {
		t.Fatalf("out=%q", out)
	}
}

func TestDiffFindsBodyRegression(t *testing.T) {
	a, b := twin(t,
		jsonHandler(200, `{"role":"admin"}`, nil),
		jsonHandler(200, `{"role":"administrator"}`, nil))
	code, out, _ := run("diff", "--a", a, "--b", b, "/x")
	if code != ExitDiff {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, "$.role") || !strings.Contains(out, "result: DIFF — 1 difference") {
		t.Fatalf("out=%q", out)
	}
}

func TestDiffStatusMismatch(t *testing.T) {
	a, b := twin(t,
		jsonHandler(200, `{}`, nil),
		jsonHandler(404, `{}`, nil))
	code, out, _ := run("diff", "--a", a, "--b", b, "/x")
	if code != ExitDiff || !strings.Contains(out, "status") ||
		!strings.Contains(out, "a: 200") || !strings.Contains(out, "b: 404") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestIgnoreTimestampsAndIDsReachParity(t *testing.T) {
	a, b := twin(t,
		jsonHandler(200, `{"v":1,"at":"2026-07-12T10:00:00.000Z","rid":"7f9c24e5-3b1a-4d2e-9c8f-1a2b3c4d5e6f"}`, nil),
		jsonHandler(200, `{"v":1,"at":"2026-07-12T10:00:07Z","rid":"0d9e8f7a-6b5c-4d3e-2f1a-0b9c8d7e6f5a"}`, nil))
	// Without filters: two differences.
	code, _, _ := run("diff", "--a", a, "--b", b, "/x")
	if code != ExitDiff {
		t.Fatalf("unfiltered code=%d, want 1", code)
	}
	// With filters: parity, and the note says what was suppressed
	// (timestamp + id + the content-length header, since body sizes differ).
	code, out, _ := run("diff", "--a", a, "--b", b,
		"--ignore-timestamps", "--ignore-ids", "/x")
	if code != ExitParity || !strings.Contains(out, "PARITY (3 ignored as noise)") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestIgnorePathFlagIsRepeatable(t *testing.T) {
	a, b := twin(t,
		jsonHandler(200, `{"m":{"x":1},"n":2,"keep":3}`, nil),
		jsonHandler(200, `{"m":{"x":9},"n":5,"keep":3}`, nil))
	code, _, _ := run("diff", "--a", a, "--b", b,
		"--ignore", "$.m", "--ignore", "$.n", "/x")
	if code != ExitParity {
		t.Fatalf("code=%d, want parity after ignoring both paths", code)
	}
}

func TestJSONFormatIsMachineReadable(t *testing.T) {
	a, b := twin(t,
		jsonHandler(200, `{"total":2}`, nil),
		jsonHandler(200, `{"total":"2"}`, nil))
	code, out, _ := run("diff", "--a", a, "--b", b, "--format", "json", "/x")
	if code != ExitDiff {
		t.Fatalf("code=%d", code)
	}
	var doc struct {
		Tool          string `json:"tool"`
		SchemaVersion int    `json:"schema_version"`
		Results       []struct {
			Differences []struct {
				Path    string `json:"path"`
				Kind    string `json:"kind"`
				Ignored bool   `json:"ignored"`
			} `json:"differences"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if doc.Tool != "twinget" || doc.SchemaVersion != 1 {
		t.Fatalf("envelope: %+v", doc)
	}
	// The type regression must be present as an effective difference
	// (ignored header noise like content-length may precede it).
	found := false
	for _, d := range doc.Results[0].Differences {
		if d.Path == "$.total" && d.Kind == "type" && !d.Ignored {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing $.total type difference: %s", out)
	}
}

func TestMarkdownFormat(t *testing.T) {
	a, b := twin(t,
		jsonHandler(200, `{"x":1}`, nil),
		jsonHandler(200, `{"x":2}`, nil))
	code, out, _ := run("diff", "--a", a, "--b", b, "--format", "markdown", "/x")
	if code != ExitDiff || !strings.Contains(out, "## twinget parity report") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestRequestShapingFlagsReachBothBackends(t *testing.T) {
	// Each backend echoes method, body, and the custom header back into
	// its response body; if both receive identical requests, the run is
	// parity — so parity here proves faithful mirroring of -X/-H/-d.
	echo := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r.Body)
		_, _ = w.Write([]byte(`{"method":"` + r.Method +
			`","env":"` + r.Header.Get("X-Env") +
			`","body":` + `"` + strings.ReplaceAll(buf.String(), `"`, `'`) + `"}`))
	}
	a, b := twin(t, http.HandlerFunc(echo), http.HandlerFunc(echo))
	code, out, _ := run("diff", "--a", a, "--b", b,
		"-X", "POST", "-H", "X-Env: staging", "-d", `{"sku":"TWG-1"}`, "/orders")
	if code != ExitParity {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestTransportFailureExitsThreeAndNamesTheSide(t *testing.T) {
	h := jsonHandler(200, `{}`, nil)
	a, _ := twin(t, h, h)
	code, _, errOut := run("diff", "--a", a, "--b", "http://127.0.0.1:1", "/x")
	if code != ExitRuntime {
		t.Fatalf("code=%d, want 3", code)
	}
	if !strings.Contains(errOut, "backend b") {
		t.Fatalf("stderr should blame side b: %q", errOut)
	}
}

func TestUsageErrors(t *testing.T) {
	cases := [][]string{
		{},             // no arguments at all
		{"frobnicate"}, // unknown subcommand
		{"diff", "/x"}, // missing --a/--b
		{"diff", "--a", "http://127.0.0.1:1", "--b", "nope", "/x"}, // bad URL scheme
		{"diff", "--a", "http://127.0.0.1:1", "--b", "http://127.0.0.1:2", "--format", "yaml", "/x"},
		{"diff", "--a", "http://127.0.0.1:1", "--b", "http://127.0.0.1:2", "--ignore", "$.a[", "/x"},
		{"diff", "--a", "http://127.0.0.1:1", "--b", "http://127.0.0.1:2", "no-slash"},
		{"diff", "--a", "http://127.0.0.1:1", "--b", "http://127.0.0.1:2"}, // no path at all
		{"batch", "--a", "http://127.0.0.1:1", "--b", "http://127.0.0.1:2",
			"/definitely/not/here.txt"}, // missing batch file
	}
	for _, argv := range cases {
		code, _, errOut := run(argv...)
		if code != ExitUsage {
			t.Fatalf("%v -> code=%d (stderr %q), want 2", argv, code, errOut)
		}
		if errOut == "" {
			t.Fatalf("%v -> empty stderr", argv)
		}
	}
}

func TestBatchMixedResults(t *testing.T) {
	mux := func(users string) *http.ServeMux {
		m := http.NewServeMux()
		m.Handle("/api/users", jsonHandler(200, users, nil))
		m.Handle("/api/health", jsonHandler(200, `{"status":"ok"}`, nil))
		return m
	}
	a, b := twin(t, mux(`{"n":1}`), mux(`{"n":2}`))

	dir := t.TempDir()
	file := filepath.Join(dir, "requests.txt")
	content := "# demo batch\nGET /api/users\n/api/health\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run("batch", "--a", a, "--b", b, file)
	if code != ExitDiff {
		t.Fatalf("code=%d out=%q", code, code)
	}
	for _, want := range []string{
		"DIFF", "/api/users",
		"ok", "/api/health",
		"2 requests: 1 parity, 1 diff — FAIL",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestBatchAllParityExitsZero(t *testing.T) {
	h := jsonHandler(200, `{"ok":true}`, nil)
	a, b := twin(t, h, h)
	dir := t.TempDir()
	file := filepath.Join(dir, "requests.txt")
	if err := os.WriteFile(file, []byte("/x\n/y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run("batch", "--a", a, "--b", b, file)
	if code != ExitParity || !strings.Contains(out, "2 requests: 2 parity, 0 diff — OK") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestBatchJSONLineWithQueryAndHeaders(t *testing.T) {
	seen := ""
	h := func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.RequestURI() + "|" + r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}
	a, b := twin(t, http.HandlerFunc(h), http.HandlerFunc(h))
	dir := t.TempDir()
	file := filepath.Join(dir, "requests.txt")
	line := `{"method":"GET","path":"/api/users?limit=1","headers":{"Accept":"application/json"}}` + "\n"
	if err := os.WriteFile(file, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, _ := run("batch", "--a", a, "--b", b, file)
	if code != ExitParity {
		t.Fatalf("code=%d", code)
	}
	if seen != "/api/users?limit=1|application/json" {
		t.Fatalf("request not faithful: %q", seen)
	}
}

func TestStrictHeadersSurfacesServerHeader(t *testing.T) {
	a, b := twin(t,
		jsonHandler(200, `{}`, map[string]string{"Server": "legacy-node/14.21"}),
		jsonHandler(200, `{}`, map[string]string{"Server": "go-rewrite/2.0"}))
	code, _, _ := run("diff", "--a", a, "--b", b, "/x")
	if code != ExitParity {
		t.Fatalf("default mode should ignore Server, code=%d", code)
	}
	code, out, _ := run("diff", "--a", a, "--b", b, "--strict-headers", "/x")
	if code != ExitDiff || !strings.Contains(out, "header server") {
		t.Fatalf("strict mode: code=%d out=%q", code, out)
	}
}

func TestUnorderedFlagOnCLI(t *testing.T) {
	a, b := twin(t,
		jsonHandler(200, `{"tags":["a","b"]}`, nil),
		jsonHandler(200, `{"tags":["b","a"]}`, nil))
	code, _, _ := run("diff", "--a", a, "--b", b, "/x")
	if code != ExitDiff {
		t.Fatalf("ordered mode should flag reorder, code=%d", code)
	}
	code, _, _ = run("diff", "--a", a, "--b", b, "--unordered", "$.tags", "/x")
	if code != ExitParity {
		t.Fatalf("unordered mode should be parity, code=%d", code)
	}
}

func TestShowIgnoredOnCLI(t *testing.T) {
	a, b := twin(t,
		jsonHandler(200, `{"at":"2026-07-12T10:00:00Z"}`, nil),
		jsonHandler(200, `{"at":"2026-07-12T10:00:07Z"}`, nil))
	code, out, _ := run("diff", "--a", a, "--b", b,
		"--ignore-timestamps", "--show-ignored", "/x")
	if code != ExitParity || !strings.Contains(out, "(ignored: timestamp noise)") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestNonJSONBodiesStillDiff(t *testing.T) {
	plain := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(body))
		}
	}
	a, b := twin(t, plain("pong\n"), plain("PONG\n"))
	code, out, _ := run("diff", "--a", a, "--b", b, "/ping")
	if code != ExitDiff || !strings.Contains(out, "text") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}
