// Package request models the request twinget mirrors to both backends
// and parses batch files: one request per line, either "METHOD /path"
// shorthand or a JSON object for full control.
package request

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Spec is one request to send to both backends. Path may carry a query
// string; Body is sent verbatim.
type Spec struct {
	Method  string
	Path    string
	Headers [][2]string // ordered, repeatable header pairs
	Body    []byte
}

// knownMethods gates the "METHOD /path" shorthand so a stray word at
// the start of a line fails loudly instead of becoming a bogus method.
var knownMethods = map[string]bool{
	"GET": true, "HEAD": true, "POST": true, "PUT": true,
	"PATCH": true, "DELETE": true, "OPTIONS": true,
}

// ParseHeaderFlag splits a curl-style "Name: value" -H argument.
func ParseHeaderFlag(raw string) (name, value string, err error) {
	colon := strings.IndexByte(raw, ':')
	if colon <= 0 {
		return "", "", fmt.Errorf("header %q: want \"Name: value\"", raw)
	}
	name = strings.TrimSpace(raw[:colon])
	value = strings.TrimSpace(raw[colon+1:])
	if name == "" {
		return "", "", fmt.Errorf("header %q: empty name", raw)
	}
	return name, value, nil
}

// jsonLine is the JSON-object form of one batch entry.
type jsonLine struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

// ParseBatch reads a batch file: blank lines and #-comments are
// skipped, lines starting with "{" are JSON specs, everything else is
// "METHOD /path" or a bare "/path" (implied GET).
func ParseBatch(r io.Reader) ([]Spec, error) {
	var specs []Spec
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		spec, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		specs = append(specs, spec)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("batch file has no requests")
	}
	return specs, nil
}

func parseLine(line string) (Spec, error) {
	if strings.HasPrefix(line, "{") {
		return parseJSONLine(line)
	}
	fields := strings.Fields(line)
	switch len(fields) {
	case 1:
		if !strings.HasPrefix(fields[0], "/") {
			return Spec{}, fmt.Errorf("path %q must start with '/'", fields[0])
		}
		return Spec{Method: "GET", Path: fields[0]}, nil
	case 2:
		method := strings.ToUpper(fields[0])
		if !knownMethods[method] {
			return Spec{}, fmt.Errorf("unknown method %q", fields[0])
		}
		if !strings.HasPrefix(fields[1], "/") {
			return Spec{}, fmt.Errorf("path %q must start with '/'", fields[1])
		}
		return Spec{Method: method, Path: fields[1]}, nil
	default:
		return Spec{}, fmt.Errorf("want \"METHOD /path\" or a JSON object, got %q", line)
	}
}

// parseJSONLine decodes the JSON form. A string body is sent verbatim;
// any other JSON body is re-marshaled compactly and, when no explicit
// Content-Type is given, sent as application/json.
func parseJSONLine(line string) (Spec, error) {
	dec := json.NewDecoder(strings.NewReader(line))
	dec.DisallowUnknownFields()
	var jl jsonLine
	if err := dec.Decode(&jl); err != nil {
		return Spec{}, fmt.Errorf("bad JSON request: %v", err)
	}
	if jl.Path == "" || !strings.HasPrefix(jl.Path, "/") {
		return Spec{}, fmt.Errorf("JSON request needs a \"path\" starting with '/'")
	}
	spec := Spec{Method: strings.ToUpper(jl.Method), Path: jl.Path}
	if spec.Method == "" {
		spec.Method = "GET"
	}
	if !knownMethods[spec.Method] {
		return Spec{}, fmt.Errorf("unknown method %q", jl.Method)
	}
	// Ordered, deterministic header emission.
	names := make([]string, 0, len(jl.Headers))
	for name := range jl.Headers {
		names = append(names, name)
	}
	sort.Strings(names)
	hasContentType := false
	for _, name := range names {
		spec.Headers = append(spec.Headers, [2]string{name, jl.Headers[name]})
		if strings.EqualFold(name, "Content-Type") {
			hasContentType = true
		}
	}
	if len(jl.Body) > 0 {
		var s string
		if err := json.Unmarshal(jl.Body, &s); err == nil {
			spec.Body = []byte(s)
		} else {
			compact, err := compactJSON(jl.Body)
			if err != nil {
				return Spec{}, fmt.Errorf("bad body: %v", err)
			}
			spec.Body = compact
			if !hasContentType {
				spec.Headers = append(spec.Headers, [2]string{"Content-Type", "application/json"})
			}
		}
	}
	return spec, nil
}

func compactJSON(raw json.RawMessage) ([]byte, error) {
	var b strings.Builder
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(b.String(), "\n")), nil
}
