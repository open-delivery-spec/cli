// Package mutation ingests a mutation-testing report (gremlins JSON) and
// computes a diff-scoped mutation score: of the mutants injected on the change's
// added lines, how many the test suite killed. It answers the deterministic
// question patch coverage cannot — "are the tests real, or do they run the code
// without asserting on it?" ODS is a signal consumer here, not a test runner:
// the team runs the mutation tool in CI and points `ods check --mutation` at the
// report, exactly as it does for `--sarif` and `--coverage`.
package mutation

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// NotMeasured is the sentinel score when no report is available or no mutant
// falls on a changed line. Mirrors coverage.NotMeasured so policies guard the
// same way (`input.mutation_score >= 0`).
const NotMeasured = -1.0

// Report is the subset of gremlins' JSON output (`gremlins unleash --output`)
// that ODS consumes. Extra fields in the file are ignored.
type Report struct {
	Files []FileMutations `json:"files"`
}

// FileMutations is the per-file mutant list.
type FileMutations struct {
	FileName  string     `json:"file_name"`
	Mutations []Mutation `json:"mutations"`
}

// Mutation is one injected mutant and the test suite's verdict on it.
type Mutation struct {
	Line   int    `json:"line"`
	Status string `json:"status"`
}

// Parse reads a gremlins JSON report from path.
func Parse(path string) (*Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parsing mutation report %s: %w", path, err)
	}
	return &r, nil
}

// killed reports whether a mutant status counts as caught. A timed-out mutant
// made the tests hang/fail, so the change was detected — it counts as killed.
func killed(status string) bool {
	s := strings.ToUpper(status)
	return strings.Contains(s, "KILL") || strings.Contains(s, "TIMED")
}

// lived reports whether a mutant survived the test suite (an escape).
func lived(status string) bool {
	s := strings.ToUpper(status)
	// Guard against "NOT VIABLE"/"NOT COVERED" which also start elsewhere; only
	// an explicit LIVED status is an escape.
	return strings.Contains(s, "LIVED") || s == "LIVE"
}

// DiffScopedMSI computes the mutation score indicator over mutants whose
// (file, line) fall on the diff's added lines: killed / (killed + lived).
// Mutants that are not viable or not covered are excluded from the denominator
// (they say nothing about test strength). addedByFile maps a repo-relative diff
// path to its added new-file line numbers. Returns (killed, killed+lived).
func (r *Report) DiffScopedMSI(addedByFile map[string][]int) (killedN, total int) {
	if r == nil {
		return 0, 0
	}
	for _, fm := range r.Files {
		lines := matchFile(fm.FileName, addedByFile)
		if lines == nil {
			continue
		}
		for _, m := range fm.Mutations {
			if _, ok := lines[m.Line]; !ok {
				continue
			}
			switch {
			case killed(m.Status):
				killedN++
				total++
			case lived(m.Status):
				total++
			}
		}
	}
	return killedN, total
}

// matchFile maps a report's file path to the changed-line set of the diff file
// it best matches. Report paths are module-relative (e.g. internal/x/f.go) and
// diff paths repo-relative; prefer the longest diff path that is a suffix of the
// report path (or vice versa), then fall back to a unique basename match. Mirror
// of coverage.matchFile. Returns a set (line → struct{}) for O(1) membership.
func matchFile(reportPath string, addedByFile map[string][]int) map[int]struct{} {
	best := ""
	for diffPath := range addedByFile {
		if reportPath == diffPath ||
			strings.HasSuffix(reportPath, "/"+diffPath) ||
			strings.HasSuffix(diffPath, "/"+reportPath) {
			if len(diffPath) > len(best) {
				best = diffPath
			}
		}
	}
	if best == "" {
		// Fallback: unique basename match.
		base := basename(reportPath)
		matches := 0
		for diffPath := range addedByFile {
			if basename(diffPath) == base {
				best = diffPath
				matches++
			}
		}
		if matches != 1 {
			return nil
		}
	}
	set := make(map[int]struct{}, len(addedByFile[best]))
	for _, ln := range addedByFile[best] {
		set[ln] = struct{}{}
	}
	return set
}

func basename(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
