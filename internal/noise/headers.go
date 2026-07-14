// Header noise: response headers that differ between any two healthy
// deployments and say nothing about API parity.
package noise

import "strings"

// defaultIgnoredHeaders lists headers twinget skips unless
// --strict-headers is set. Grouped by why they are noise:
//
//   - per-response entropy: request/trace ids, ETag revalidators
//   - clocks: Date, Age, X-Runtime, X-Response-Time
//   - transport plumbing: hop-by-hop and framing headers whose value
//     depends on the server stack, not the API contract
//   - deployment identity: Server, Via, X-Powered-By, CDN markers
//   - sessions: Set-Cookie carries fresh tokens on every response
var defaultIgnoredHeaders = map[string]bool{
	"age":               true,
	"cf-ray":            true,
	"connection":        true,
	"content-length":    true,
	"date":              true,
	"etag":              true,
	"keep-alive":        true,
	"last-modified":     true,
	"server":            true,
	"set-cookie":        true,
	"traceparent":       true,
	"transfer-encoding": true,
	"via":               true,
	"x-amzn-requestid":  true,
	"x-amzn-trace-id":   true,
	"x-correlation-id":  true,
	"x-powered-by":      true,
	"x-request-id":      true,
	"x-response-time":   true,
	"x-runtime":         true,
	"x-trace-id":        true,
}

// DefaultIgnoredHeader reports whether name (any case) is in the
// built-in header noise set.
func DefaultIgnoredHeader(name string) bool {
	return defaultIgnoredHeaders[strings.ToLower(name)]
}

// DefaultIgnoredHeaderCount returns the size of the built-in set, used
// by docs tests to keep docs/noise-filters.md honest.
func DefaultIgnoredHeaderCount() int { return len(defaultIgnoredHeaders) }
