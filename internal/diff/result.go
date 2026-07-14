// Result types shared by the CLI and every report renderer.
package diff

// Side captures what one backend returned for one request.
type Side struct {
	URL        string  `json:"url"`
	Status     int     `json:"status"`
	DurationMS float64 `json:"duration_ms"`
	BodyBytes  int     `json:"body_bytes"`
}

// RequestResult is the full comparison outcome for one request.
type RequestResult struct {
	Method      string       `json:"method"`
	Path        string       `json:"path"`
	A           Side         `json:"a"`
	B           Side         `json:"b"`
	Differences []Difference `json:"differences"`
}

// Counts returns how many differences are effective (real) and how
// many were neutralized by noise filters.
func (r RequestResult) Counts() (effective, ignored int) {
	for _, d := range r.Differences {
		if d.Ignored {
			ignored++
		} else {
			effective++
		}
	}
	return effective, ignored
}

// Parity reports whether the two backends agree once noise is removed.
func (r RequestResult) Parity() bool {
	effective, _ := r.Counts()
	return effective == 0
}
