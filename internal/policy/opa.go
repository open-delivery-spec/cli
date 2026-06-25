// Package policy provides OPA-based enterprise policy evaluation.
// It embeds the OPA Rego engine for evaluating user-defined policies
// against ODS detection and analysis results.
package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/open-policy-agent/opa/rego"
)

// EvalResult is the output of a policy evaluation.
type EvalResult struct {
	Allowed bool     `json:"allowed"`
	Denials []string `json:"denials,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// EvalInput is the data passed to Rego policies.
// TestCoverage is −1 when coverage was not measured; policies that check
// coverage MUST guard with `input.test_coverage >= 0`.
type EvalInput struct {
	AIGenerated        bool           `json:"ai_generated"`
	AIConfidence       float64        `json:"ai_confidence"`
	AIFiles            []EvalFileInfo `json:"ai_files"`
	Issues             []EvalIssue    `json:"issues"`
	TechnicalDebtDelta float64        `json:"technical_debt_delta"`
	TestCoverage       float64        `json:"test_coverage"`
	TestCoverageSource string         `json:"test_coverage_source,omitempty"`
	ChangedFiles       []string       `json:"changed_files"`
	Branch             string         `json:"branch"`
	Committer          string         `json:"committer"`
}

// EvalFileInfo describes an AI-detected file.
type EvalFileInfo struct {
	Path        string  `json:"path"`
	AILines     int     `json:"ai_lines"`
	TotalLines  int     `json:"total_lines"`
	Confidence  float64 `json:"confidence"`
}

// EvalIssue represents a quality issue.
type EvalIssue struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Evaluate evaluates a Rego policy file against the given input.
// The policy file at path should contain a package ods.policy with:
//
//	deny[msg] { ... }
//	warn[msg] { ... }
//	default allow = true
func Evaluate(policyPath string, input *EvalInput) (*EvalResult, error) {
	data, err := os.ReadFile(policyPath)
	if err != nil {
		return nil, fmt.Errorf("reading policy file %s: %w", policyPath, err)
	}

	// Convert input to map for Rego
	inputMap := make(map[string]interface{})
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshaling input: %w", err)
	}
	if err := json.Unmarshal(raw, &inputMap); err != nil {
		return nil, fmt.Errorf("unmarshaling input: %w", err)
	}

	ctx := context.Background()

	// Build query for deny, warn, and allow rules
	// OPA v1 requires a variable binding in the query (e.g., "x = data.ods.policy")
	query, err := rego.New(
		rego.Query("result = data.ods.policy"),
		rego.Module("policy.rego", string(data)),
	).PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("preparing policy: %w", err)
	}

	results, err := query.Eval(ctx, rego.EvalInput(inputMap))
	if err != nil {
		return nil, fmt.Errorf("evaluating policy: %w", err)
	}

	return parseRegoResults(results)
}

// parseRegoResults extracts deny, warn, and allow from Rego evaluation.
func parseRegoResults(results rego.ResultSet) (*EvalResult, error) {
	out := &EvalResult{Allowed: true}

	if len(results) == 0 {
		return out, nil
	}

	// The query is "result = data.ods.policy" — look for "result" key
	var policyMap map[string]interface{}

	for _, key := range []string{"result", "x", "data.ods.policy"} {
		if raw, ok := results[0].Bindings[key]; ok {
			if m, ok := raw.(map[string]interface{}); ok {
				policyMap = m
				break
			}
		}
	}

	// Fallback: try any map value in bindings
	if policyMap == nil {
		for _, v := range results[0].Bindings {
			if m, ok := v.(map[string]interface{}); ok {
				policyMap = m
				break
			}
		}
	}

	if policyMap == nil {
		return out, nil
	}

	// Extract deny
	if denyRaw, ok := policyMap["deny"]; ok {
		out.Denials = extractStringList(denyRaw)
	}

	// Extract warn
	if warnRaw, ok := policyMap["warn"]; ok {
		out.Warnings = extractStringList(warnRaw)
	}

	// Extract allow
	if allowRaw, ok := policyMap["allow"]; ok {
		if allowBool, ok := allowRaw.(bool); ok {
			out.Allowed = allowBool
		}
	}

	if len(out.Denials) > 0 {
		out.Allowed = false
	}

	return out, nil
}

// extractStringList converts various Rego result types to []string.
func extractStringList(raw interface{}) []string {
	var result []string

	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
	case map[string]interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
	case string:
		result = append(result, v)
	}

	return result
}

// DefaultRegoPolicy returns a default built-in policy for ODS.
func DefaultRegoPolicy() string {
	return `package ods.policy

# Default ODS policy: block changes with critical issues or high tech debt
default allow := true

deny[msg] {
    input.issues[_].severity == "critical"
    msg = sprintf("CRITICAL issue: %s in %s", [input.issues[_].rule, input.issues[_].file])
}

deny[msg] {
    input.technical_debt_delta > 5.0
    msg = sprintf("Technical debt increase %.1f exceeds threshold (5.0)", [input.technical_debt_delta * 1.0])
}

warn[msg] {
    input.ai_generated == true
    input.ai_confidence > 0.8
    count(input.issues) > 2
    msg = "High-confidence AI code with multiple quality issues — enhanced review recommended"
}

warn[msg] {
    input.ai_generated == true
    input.test_coverage >= 0
    input.test_coverage < 0.3
    pct := round(input.test_coverage * 100)
    msg = sprintf("AI-generated code has only %d%% test coverage", [pct])
}
`
}

// EnterpriseRegoPolicy returns an enterprise-grade policy example.
func EnterpriseRegoPolicy() string {
	return `package ods.policy

default allow := true

# Rule 1: Block critical issues unconditionally
deny[msg] {
    issue := input.issues[_]
    issue.severity == "critical"
    msg = sprintf("CRITICAL: %s at %s:%d — %s", [issue.rule, issue.file, issue.line, issue.message])
}

# Rule 2: Payment/auth module AI code requires L3-equivalent review (test coverage ≥ 60%)
deny[msg] {
    file := input.ai_files[_]
    regex.match(".*(payment|auth|billing).*", file.path)
    file.confidence > 0.5
    input.test_coverage >= 0
    input.test_coverage < 0.6
    pct := round(input.test_coverage * 100)
    msg = sprintf("AI code in sensitive module %s has %d%% test coverage (min 60%%)", [file.path, pct])
}

# Rule 3: Block high tech debt delta
deny[msg] {
    input.technical_debt_delta > 5.0
    msg = sprintf("Technical debt increase %.1f exceeds block threshold", [input.technical_debt_delta * 1.0])
}

# Rule 4: Warn on high-confidence AI with no tests
warn[msg] {
    input.ai_generated == true
    input.ai_confidence > 0.7
    input.test_coverage >= 0
    input.test_coverage < 0.2
    ai_pct := round(input.ai_confidence * 100)
    test_pct := round(input.test_coverage * 100)
    msg = sprintf("High-confidence AI code (%d%%) with low test coverage (%d%%)", [ai_pct, test_pct])
}

# Rule 5: Warn on high defect density
warn[msg] {
    count(input.issues) > 5
    msg = sprintf("High defect count: %d issues found", [count(input.issues)])
}
`
}

// DiscoverRegoFile finds a Rego policy file in the repository.
func DiscoverRegoFile(repoRoot string) string {
	paths := []string{
		repoRoot + "/.ods/policy.rego",
		repoRoot + "/.ods.rego",
		repoRoot + "/policy.rego",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// buildInputsMap is a helper for tests.
func buildInputsMap(input *EvalInput) map[string]interface{} {
	// Simple map construction for basic inputs
	return map[string]interface{}{
		"input": map[string]interface{}{
			"ai_generated":          input.AIGenerated,
			"ai_confidence":         input.AIConfidence,
			"technical_debt_delta":  input.TechnicalDebtDelta,
			"test_coverage":         input.TestCoverage,
			"branch":                input.Branch,
			"committer":             input.Committer,
			"changed_files":         input.ChangedFiles,
		},
	}
}

// Ensure strings package is used
var _ = strings.TrimSpace
