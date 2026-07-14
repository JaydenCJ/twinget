// Tests for the twin HTTP client, against in-process loopback servers
// only (httptest). No external network is ever touched.
package fetch

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JaydenCJ/twinget/internal/request"
)

func twinServers(t *testing.T, ha, hb http.HandlerFunc) (a, b *httptest.Server) {
	t.Helper()
	a = httptest.NewServer(ha)
	b = httptest.NewServer(hb)
	t.Cleanup(a.Close)
	t.Cleanup(b.Close)
	return a, b
}

func TestPairCapturesStatusHeadersAndBody(t *testing.T) {
	handler := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Side", body)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
	}
	sa, sb := twinServers(t, handler(200, "alpha"), handler(201, "beta"))
	c := New(2*time.Second, 1<<20)
	ra, rb, err := c.Pair(request.Spec{Method: "GET", Path: "/x"}, sa.URL, sb.URL)
	if err != nil {
		t.Fatal(err)
	}
	if ra.Status != 200 || string(ra.Body) != "alpha" || ra.Header.Get("X-Side") != "alpha" {
		t.Fatalf("side a: %+v", ra)
	}
	if rb.Status != 201 || string(rb.Body) != "beta" {
		t.Fatalf("side b: %+v", rb)
	}
}

func TestRequestMethodHeadersAndBodyAreMirrored(t *testing.T) {
	type seen struct {
		method, path, env, body string
	}
	record := func(dst *seen) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			buf, _ := io.ReadAll(r.Body)
			*dst = seen{r.Method, r.URL.RequestURI(), r.Header.Get("X-Env"), string(buf)}
		}
	}
	var sawA, sawB seen
	sa, sb := twinServers(t, record(&sawA), record(&sawB))
	spec := request.Spec{
		Method:  "POST",
		Path:    "/api/orders?dry=1",
		Headers: [][2]string{{"X-Env", "staging"}},
		Body:    []byte(`{"sku":"TWG-1"}`),
	}
	c := New(2*time.Second, 1<<20)
	if _, _, err := c.Pair(spec, sa.URL, sb.URL); err != nil {
		t.Fatal(err)
	}
	for _, saw := range []seen{sawA, sawB} {
		if saw.method != "POST" || saw.path != "/api/orders?dry=1" ||
			saw.env != "staging" || saw.body != `{"sku":"TWG-1"}` {
			t.Fatalf("request not mirrored faithfully: %+v", saw)
		}
	}
}

func TestUserAgentIdentifiesTwinget(t *testing.T) {
	var ua string
	h := func(w http.ResponseWriter, r *http.Request) { ua = r.Header.Get("User-Agent") }
	sa, sb := twinServers(t, h, h)
	c := New(2*time.Second, 1<<20)
	_, _, _ = c.Pair(request.Spec{Method: "GET", Path: "/"}, sa.URL, sb.URL)
	if !strings.HasPrefix(ua, "twinget/") {
		t.Fatalf("User-Agent = %q", ua)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	// A 301 on one side IS the finding; following it would hide it.
	redirect := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/moved", http.StatusMovedPermanently)
	}
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }
	sa, sb := twinServers(t, redirect, ok)
	c := New(2*time.Second, 1<<20)
	ra, rb, err := c.Pair(request.Spec{Method: "GET", Path: "/"}, sa.URL, sb.URL)
	if err != nil {
		t.Fatal(err)
	}
	if ra.Status != 301 || rb.Status != 200 {
		t.Fatalf("status = %d/%d, want 301/200", ra.Status, rb.Status)
	}
}

func TestConnectionRefusedNamesTheFailingSide(t *testing.T) {
	sa, _ := twinServers(t,
		func(w http.ResponseWriter, r *http.Request) {},
		func(w http.ResponseWriter, r *http.Request) {})
	c := New(2*time.Second, 1<<20)
	// Port 1 on loopback is essentially never listening.
	_, _, err := c.Pair(request.Spec{Method: "GET", Path: "/x"}, sa.URL, "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected a transport error")
	}
	se, ok := err.(*SideError)
	if !ok || se.Side != "b" {
		t.Fatalf("error should blame side b: %v", err)
	}
}

func TestBodyLargerThanCapIsAnErrorNotATruncatedDiff(t *testing.T) {
	big := func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 4096))
	}
	sa, sb := twinServers(t, big, big)
	c := New(2*time.Second, 1024)
	_, _, err := c.Pair(request.Spec{Method: "GET", Path: "/"}, sa.URL, sb.URL)
	if err == nil || !strings.Contains(err.Error(), "max-body-size") {
		t.Fatalf("err = %v", err)
	}
}

func TestJoinURLHandlesSlashesAndPrefixes(t *testing.T) {
	cases := []struct{ base, path, want string }{
		{"http://127.0.0.1:8801", "/users", "http://127.0.0.1:8801/users"},
		{"http://127.0.0.1:8801/", "/users", "http://127.0.0.1:8801/users"},
		{"http://127.0.0.1:8801/v1", "/users?x=1", "http://127.0.0.1:8801/v1/users?x=1"},
		{"http://127.0.0.1:8801/v1/", "users", "http://127.0.0.1:8801/v1/users"},
	}
	for _, c := range cases {
		if got := JoinURL(c.base, c.path); got != c.want {
			t.Fatalf("JoinURL(%q, %q) = %q, want %q", c.base, c.path, got, c.want)
		}
	}
}
