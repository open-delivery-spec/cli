// Package review parses AI code-review verdicts (ods.dev/review-verdict/v1).
//
// AI reviewers — Copilot code review, CodeRabbit, Claude Code's /review —
// cover the semantic layer static analysis cannot: is the logic right, does
// the change do what it claims. Their conclusions are probabilistic and can
// be steered by the code under review, so ODS treats them differently from
// deterministic findings: by default they may only *tighten* the gate
// (routing more human attention), never loosen it, and never deny — unless a
// team's policy explicitly opts in. The LLM runs outside the gate; the gate
// stays deterministic.
package review

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SchemaV1 identifies the version-1 verdict shape.
const SchemaV1 = "ods.dev/review-verdict/v1"

// Valid verdict values.
const (
	VerdictApprove        = "approve"
	VerdictRequestChanges = "request_changes"
	VerdictComment        = "comment"
)

// Verdict is one AI reviewer's conclusion about a change.
type Verdict struct {
	Schema   string    `json:"schema"`
	Reviewer Reviewer  `json:"reviewer"`
	HeadSHA  string    `json:"head_sha,omitempty"`
	Verdict  string    `json:"verdict"`
	Findings []Finding `json:"findings,omitempty"`
}

// Reviewer identifies the tool (and model) that produced the verdict.
type Reviewer struct {
	Tool  string `json:"tool"`
	Model string `json:"model,omitempty"`
}

// Finding is one semantic observation from the reviewer.
type Finding struct {
	File       string `json:"file,omitempty"`
	Line       int    `json:"line,omitempty"`
	Severity   string `json:"severity,omitempty"` // info/low/medium/high/critical
	Category   string `json:"category,omitempty"` // correctness/design/security/...
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// Load reads and validates a verdict file.
func Load(path string) (*Verdict, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading review verdict: %w", err)
	}
	var v Verdict
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("parsing review verdict %s: %w", path, err)
	}
	if !strings.HasPrefix(v.Schema, "ods.dev/review-verdict/") {
		return nil, fmt.Errorf("%s: unrecognized schema %q (want %s)", path, v.Schema, SchemaV1)
	}
	switch v.Verdict {
	case VerdictApprove, VerdictRequestChanges, VerdictComment:
	default:
		return nil, fmt.Errorf("%s: invalid verdict %q (want approve, request_changes, or comment)", path, v.Verdict)
	}
	if v.Reviewer.Tool == "" {
		return nil, fmt.Errorf("%s: reviewer.tool is required", path)
	}
	return &v, nil
}

// MatchesHead reports whether the verdict applies to the given commit SHA.
// An empty HeadSHA matches anything (the producing tool didn't stamp one);
// otherwise either value may be an abbreviation of the other. Verdicts for a
// different commit are stale opinions and must not enter the gate.
func (v *Verdict) MatchesHead(headSHA string) bool {
	if v.HeadSHA == "" || headSHA == "" {
		return true
	}
	a, b := strings.ToLower(v.HeadSHA), strings.ToLower(headSHA)
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}
