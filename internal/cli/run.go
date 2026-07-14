// Execution for the diff and batch subcommands: build the twin client,
// mirror each request, assemble results, render, pick the exit code.
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/JaydenCJ/twinget/internal/diff"
	"github.com/JaydenCJ/twinget/internal/fetch"
	"github.com/JaydenCJ/twinget/internal/report"
	"github.com/JaydenCJ/twinget/internal/request"
)

// buildSpec turns diff-subcommand flags into a request spec.
func (o *options) buildSpec(path string) (request.Spec, error) {
	if !strings.HasPrefix(path, "/") {
		return request.Spec{}, fmt.Errorf("path %q must start with '/'", path)
	}
	spec := request.Spec{Method: strings.ToUpper(o.method), Path: path}
	for _, raw := range o.headers {
		name, value, err := request.ParseHeaderFlag(raw)
		if err != nil {
			return request.Spec{}, err
		}
		spec.Headers = append(spec.Headers, [2]string{name, value})
	}
	switch {
	case o.bodyFile != "":
		data, err := os.ReadFile(o.bodyFile)
		if err != nil {
			return request.Spec{}, fmt.Errorf("--body-file: %v", err)
		}
		spec.Body = data
	case o.body != "":
		spec.Body = []byte(o.body)
	}
	return spec, nil
}

// diffOptions maps CLI flags onto the diff engine's options.
func (o *options) diffOptions() diff.Options {
	return diff.Options{
		IgnorePaths:      o.ignorePatterns,
		UnorderedPaths:   o.unorderedPatterns,
		IgnoreTimestamps: o.ignoreTimestamps,
		IgnoreIDs:        o.ignoreIDs,
		IgnoreHeaders:    o.ignoreHeaders,
		StrictHeaders:    o.strictHeaders,
	}
}

// execute mirrors one request to both backends and diffs the answers.
func (o *options) execute(client *fetch.Client, spec request.Spec) (diff.RequestResult, error) {
	a, b, err := client.Pair(spec, o.baseA, o.baseB)
	if err != nil {
		return diff.RequestResult{}, err
	}
	dopts := o.diffOptions()
	var diffs []diff.Difference
	diffs = append(diffs, diff.Status(a.Status, b.Status)...)
	diffs = append(diffs, diff.Headers(a.Header, b.Header, dopts)...)
	diffs = append(diffs, diff.Bodies(a.Body, b.Body, dopts)...)
	return diff.RequestResult{
		Method:      spec.Method,
		Path:        spec.Path,
		A:           side(a),
		B:           side(b),
		Differences: diffs,
	}, nil
}

func side(r *fetch.Result) diff.Side {
	return diff.Side{
		URL:        r.URL,
		Status:     r.Status,
		DurationMS: float64(r.Duration.Microseconds()) / 1000.0,
		BodyBytes:  len(r.Body),
	}
}

// render writes results in the requested format and returns the final
// exit code (0 parity everywhere, 1 otherwise).
func (o *options) render(w io.Writer, results []diff.RequestResult) (int, error) {
	meta := report.Meta{BaseA: o.baseA, BaseB: o.baseB, ShowIgnored: o.showIgnored}
	switch o.format {
	case "json":
		if err := report.JSON(w, meta, results); err != nil {
			return ExitRuntime, err
		}
	case "markdown":
		report.Markdown(w, meta, results)
	default:
		report.Text(w, meta, results)
	}
	if report.Summarize(results).Diff > 0 {
		return ExitDiff, nil
	}
	return ExitParity, nil
}

func runDiff(o *options, path string, stdout io.Writer) (int, error) {
	spec, err := o.buildSpec(path)
	if err != nil {
		return ExitUsage, err
	}
	client := fetch.New(o.timeout, o.maxBodySize)
	result, err := o.execute(client, spec)
	if err != nil {
		return ExitRuntime, err
	}
	return o.render(stdout, []diff.RequestResult{result})
}

func runBatch(o *options, file string, stdout io.Writer) (int, error) {
	f, err := os.Open(file)
	if err != nil {
		return ExitUsage, fmt.Errorf("batch file: %v", err)
	}
	defer f.Close()
	specs, err := request.ParseBatch(f)
	if err != nil {
		return ExitUsage, fmt.Errorf("batch file %s: %v", file, err)
	}
	client := fetch.New(o.timeout, o.maxBodySize)
	results := make([]diff.RequestResult, 0, len(specs))
	for _, spec := range specs {
		result, err := o.execute(client, spec)
		if err != nil {
			return ExitRuntime, err
		}
		results = append(results, result)
	}
	return o.render(stdout, results)
}
