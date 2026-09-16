// Package policy provides OPA-based enterprise policy evaluation.
// It embeds the OPA Rego engine for evaluating user-defined policies
// against ODS detection and analysis results.
package policy

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"

	"github.com/open-policy-agent/opa/rego"
)

// Review tiers route scarce human review attention by risk. They are advisory
// routing signals for *allowed* changes — deny always wins, so a blocked PR is
// never routed. Policies opt in by defining a `review_tier` rule; when absent,
// consumers should treat the tier as ReviewTierStandard.
const (
	// ReviewTierAuto marks a low-risk change eligible for expedited review or
	// auto-merge (per the consumer's configuration).
	ReviewTierAuto = "auto"
	// ReviewTierStandard is the default: normal review.
	ReviewTierStandard = "standard"
	// ReviewTierElevated marks a high-risk change that should get extra
	// reviewers or senior attention.
	ReviewTierElevated = "elevated"
)

// EvalResult is the output of a policy evaluation.
type EvalResult struct {
	Allowed  bool     `json:"allowed"`
	Denials  []string `json:"denials,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	// ReviewTier is the policy's review-routing verdict (auto/standard/
	// elevated). Empty when the policy defines no review_tier rule.
	ReviewTier string `json:"review_tier,omitempty"`
}

// EvalInput is the data passed to Rego policies.
// TestCoverage is −1 when coverage was not measured; policies that check
// coverage MUST guard with `input.test_coverage >= 0`.
type EvalInput struct {
	AIGenerated  bool    `json:"ai_generated"`
	AIConfidence float64 `json:"ai_confidence"`
	// DetectionSources lists which detection signals fired (commit-trailer,
	// git-ai-notes, pr-body, branch-name, diff-heuristics). It lets policies
	// separate *disclosed* AI use (author attribution: commit-trailer,
	// git-ai-notes, pr-body) from merely *suspected* AI use, so the SFC/kernel
	// disclosure norms become checkable at the gate.
	DetectionSources   []string       `json:"detection_sources,omitempty"`
	AIFiles            []EvalFileInfo `json:"ai_files"`
	Issues             []EvalIssue    `json:"issues"`
	TechnicalDebtDelta float64        `json:"technical_debt_delta"`
	TestCoverage       float64        `json:"test_coverage"`
	TestCoverageSource string         `json:"test_coverage_source,omitempty"`
	// PatchCoverage is coverage of the diff's *added* lines (0..1), or −1 when
	// not measured (no per-line coverage report, or no changed source line is
	// tracked). Stronger than TestCoverage for a PR: it asks whether *this
	// change's* new code is covered. Policies MUST guard with
	// `input.patch_coverage >= 0`.
	PatchCoverage float64 `json:"patch_coverage"`
	// MutationScore is the diff-scoped mutation score (0..1): of the mutants
	// injected on the change's added lines, the fraction the tests killed, or −1
	// when not measured (no mutation report, or no mutant on a changed line).
	// Stronger than coverage — it asks whether the tests *assert*, not just
	// execute. Ingested from a gremlins report via `ods check --mutation`.
	// Policies MUST guard with `input.mutation_score >= 0`.
	MutationScore float64  `json:"mutation_score"`
	ChangedFiles  []string `json:"changed_files"`
	Branch        string   `json:"branch"`
	// EvidenceTier is the strongest class of AI-attribution evidence present,
	// derived from DetectionSources: "corroborated" (git-ai-notes) > "attested"
	// (commit-trailer, pr-body) > "inferred" (branch-name, diff-heuristics).
	// Empty when no attribution source fired. A confidence label for how the
	// attribution was obtained — not forensic proof of authorship.
	EvidenceTier string `json:"evidence_tier,omitempty"`
	// AIReviews carries AI code-reviewer verdicts (semantic review). They are
	// kept separate from Issues on purpose: deterministic findings may deny,
	// probabilistic opinions default to routing attention only. Policies that
	// want LLM findings to block must opt in explicitly over this section.
	AIReviews []EvalAIReview `json:"ai_reviews,omitempty"`
	// MergeConfidence carries deterministic, diff-scoped facts about the change
	// (is it tested, how is it shaped, does it touch sensitive paths). These are
	// facts, not opinions — advisory by default (they route review attention),
	// and a policy may opt in to deny on them. Attribution (ai_generated) is
	// used to raise the bar, never these signals to detect AI.
	MergeConfidence *EvalMergeConfidence `json:"merge_confidence,omitempty"`
}

// EvalMergeConfidence mirrors the deterministic merge-confidence signals
// (package internal/mergeconf) as policy input under input.merge_confidence.
type EvalMergeConfidence struct {
	FilesChanged            int      `json:"files_changed"`
	SourceFilesChanged      int      `json:"source_files_changed"`
	TestFilesChanged        int      `json:"test_files_changed"`
	NetAddedLines           int      `json:"net_added_lines"`
	TestsTouched            bool     `json:"tests_touched"`
	AddedSourceWithoutTests bool     `json:"added_source_without_tests"`
	RiskyPaths              []string `json:"risky_paths,omitempty"`
}

// EvalAIReview is one AI reviewer's verdict in the policy input.
type EvalAIReview struct {
	Tool     string            `json:"tool"`
	Model    string            `json:"model,omitempty"`
	Verdict  string            `json:"verdict"` // approve | request_changes | comment
	Findings []EvalReviewIssue `json:"findings,omitempty"`
}

// EvalReviewIssue is one semantic finding from an AI reviewer.
type EvalReviewIssue struct {
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Severity string `json:"severity,omitempty"`
	Category string `json:"category,omitempty"`
	Message  string `json:"message"`
}

// EvalFileInfo describes an AI-detected file.
type EvalFileInfo struct {
	Path       string  `json:"path"`
	AILines    int     `json:"ai_lines"`
	TotalLines int     `json:"total_lines"`
	Confidence float64 `json:"confidence"`
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

	// Extract review_tier. An unknown value falls back to "standard" with a
	// warning rather than failing the pipeline — a routing typo in a policy
	// must not break the gate itself.
	if tierRaw, ok := policyMap["review_tier"]; ok {
		if tier, ok := tierRaw.(string); ok {
			switch tier {
			case ReviewTierAuto, ReviewTierStandard, ReviewTierElevated:
				out.ReviewTier = tier
			default:
				out.ReviewTier = ReviewTierStandard
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"policy returned unknown review_tier %q — falling back to %q",
					tier, ReviewTierStandard))
			}
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

//go:embed default_policy.rego
var defaultPolicyRego string

// DefaultRegoPolicy returns the built-in default policy: what `ods check`
// applies when a repository has no .ods/policy.rego, and the text `ods init`
// writes as a repository's starting policy. One source, so the two never
// diverge.
func DefaultRegoPolicy() string {
	return defaultPolicyRego
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
