// Package fetch sends the same request to both backends concurrently
// and captures status, headers, body and latency for each side.
//
// Redirects are deliberately NOT followed: a 301 from one backend and
// a 200 from the other is exactly the kind of behavioral difference a
// parity check exists to catch.
package fetch

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/JaydenCJ/twinget/internal/request"
	"github.com/JaydenCJ/twinget/internal/version"
)

// Result is one backend's answer to one request.
type Result struct {
	URL      string
	Status   int
	Header   http.Header
	Body     []byte
	Duration time.Duration
}

// SideError wraps a transport failure with the side ("a" or "b") and
// URL that failed, so error messages point at the right backend.
type SideError struct {
	Side string
	URL  string
	Err  error
}

func (e *SideError) Error() string {
	return fmt.Sprintf("backend %s (%s): %v", e.Side, e.URL, e.Err)
}

func (e *SideError) Unwrap() error { return e.Err }

// Client mirrors requests to a pair of base URLs.
type Client struct {
	httpClient  *http.Client
	maxBodySize int64
}

// New builds a Client. timeout bounds each request end-to-end;
// maxBodySize caps how many body bytes are read per side (a diff of
// two truncated bodies would lie, so exceeding the cap is an error).
func New(timeout time.Duration, maxBodySize int64) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		maxBodySize: maxBodySize,
	}
}

// JoinURL glues a base URL and a request path together without eating
// path prefixes: base "http://127.0.0.1:8801/v1" + "/users?limit=2"
// yields "http://127.0.0.1:8801/v1/users?limit=2".
func JoinURL(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

// Pair sends spec to both base URLs at the same time and returns both
// results. The first transport failure (a before b when both fail) is
// returned as a *SideError.
func (c *Client) Pair(spec request.Spec, baseA, baseB string) (a, b *Result, err error) {
	type outcome struct {
		res *Result
		err error
	}
	run := func(side, base string, ch chan<- outcome) {
		res, err := c.one(spec, base)
		if err != nil {
			err = &SideError{Side: side, URL: JoinURL(base, spec.Path), Err: err}
		}
		ch <- outcome{res, err}
	}
	chA := make(chan outcome, 1)
	chB := make(chan outcome, 1)
	go run("a", baseA, chA)
	go run("b", baseB, chB)
	oa, ob := <-chA, <-chB
	if oa.err != nil {
		return nil, nil, oa.err
	}
	if ob.err != nil {
		return nil, nil, ob.err
	}
	return oa.res, ob.res, nil
}

// one performs a single request against one backend.
func (c *Client) one(spec request.Spec, base string) (*Result, error) {
	url := JoinURL(base, spec.Path)
	var body io.Reader
	if len(spec.Body) > 0 {
		body = bytes.NewReader(spec.Body)
	}
	req, err := http.NewRequest(spec.Method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "twinget/"+version.Version)
	req.Header.Set("Accept", "application/json, */*")
	for _, h := range spec.Headers {
		req.Header.Set(h[0], h[1])
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBodySize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > c.maxBodySize {
		return nil, fmt.Errorf("body exceeds --max-body-size (%d bytes)", c.maxBodySize)
	}
	return &Result{
		URL:      url,
		Status:   resp.StatusCode,
		Header:   resp.Header,
		Body:     data,
		Duration: time.Since(start),
	}, nil
}
