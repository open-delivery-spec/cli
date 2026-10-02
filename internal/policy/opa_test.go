package policy

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/rego"
)

func TestEvaluateDefaultPolicy(t *testing.T) {
	// Write default policy to temp file
	tmpFile, err := os.CreateTemp("", "ods-policy-test-*.rego")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(DefaultRegoPolicy()); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Test 1: Clean input - should pass
	t.Run("clean input passes", func(t *testing.T) {
		input := &EvalInput{
			AIGenerated:        false,
			AIConfidence:       0,
			TechnicalDebtDelta: 0.5,
			TestCoverage:       0.8,
		}
		result, err := Evaluate(tmpFile.Name(), input)
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if !result.Allowed {
			t.Errorf("expected allowed=true, got false with denials: %v", result.Denials)
		}
	})

	// Test 2: Critical issues should block
	t.Run("critical issues block", func(t *testing.T) {
		input := &EvalInput{
			AIGenerated:        true,
			AIConfidence:       0.9,
			TechnicalDebtDelta: 1.0,
			TestCoverage:       0.5,
			Issues: []EvalIssue{
				{Rule: "ai-hallucinated-api", File: "auth.go", Line: 10, Severity: "critical", Message: "API does not exist"},
			},
		}
		result, err := Evaluate(tmpFile.Name(), input)
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if result.Allowed {
			t.Error("expected allowed=false for critical issues")
		}
	})

	// Test 3: High tech debt warns; the heuristic score never blocks on its own
	t.Run("high tech debt warns but does not block", func(t *testing.T) {
		input := &EvalInput{
			AIGenerated:        true,
			AIConfidence:       0.9,
			TechnicalDebtDelta: 6.5,
			TestCoverage:       0.5,
		}
		result, err := Evaluate(tmpFile.Name(), input)
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if !result.Allowed {
			t.Errorf("a debt score alone must not deny, got denials: %v", result.Denials)
		}
		found := false
		for _, w := range result.Warnings {
			if strings.Contains(w, "Technical debt delta") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected a technical-debt warning, got %v", result.Warnings)
		}
	})

	// Test 4: AI with low coverage warns
	t.Run("ai low coverage warns", func(t *testing.T) {
		input := &EvalInput{
			AIGenerated:        true,
			AIConfidence:       0.85,
			TechnicalDebtDelta: 1.0,
			TestCoverage:       0.15,
		}
		result, err := Evaluate(tmpFile.Name(), input)
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if !result.Allowed {
			t.Errorf("expected allowed=true for warning-only case, got denials: %v", result.Denials)
		}
		if len(result.Warnings) == 0 {
			t.Error("expected warnings for low-coverage AI code")
		}
	})
}

// enterprisePolicyFixture is a realistic strict policy used to exercise OPA
// evaluation (sensitive-module gating, coverage thresholds). It mirrors
// examples/ods-policy-enterprise.rego in the spec repo.
const enterprisePolicyFixture = `package ods.policy

default allow := true

deny[msg] {
    issue := input.issues[_]
    issue.severity == "critical"
    msg = sprintf("CRITICAL: %s at %s:%d", [issue.rule, issue.file, issue.line])
}

deny[msg] {
    file := input.ai_files[_]
    regex.match(".*(payment|auth|billing).*", file.path)
    file.confidence > 0.5
    input.test_coverage >= 0
    input.test_coverage < 0.6
    msg = sprintf("AI code in sensitive module %s under 60%% coverage", [file.path])
}
`

func TestEvaluateEnterprisePolicy(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "ods-enterprise-*.rego")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(enterprisePolicyFixture); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	t.Run("payment module AI code low coverage blocks", func(t *testing.T) {
		input := &EvalInput{
			AIGenerated:  true,
			AIConfidence: 0.9,
			TestCoverage: 0.3,
			AIFiles: []EvalFileInfo{
				{Path: "src/payment/handler.go", AILines: 50, TotalLines: 80, Confidence: 0.85},
			},
		}
		result, err := Evaluate(tmpFile.Name(), input)
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if result.Allowed {
			t.Error("expected blocked for payment module AI code with low coverage")
		}
	})

	t.Run("non-sensitive module AI code passes", func(t *testing.T) {
		input := &EvalInput{
			AIGenerated:  true,
			AIConfidence: 0.6,
			TestCoverage: 0.7,
			AIFiles: []EvalFileInfo{
				{Path: "src/utils/helpers.go", AILines: 20, TotalLines: 100, Confidence: 0.5},
			},
		}
		result, err := Evaluate(tmpFile.Name(), input)
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if !result.Allowed {
			t.Errorf("expected allowed for non-sensitive module, got denials: %v", result.Denials)
		}
	})
}

func TestParseRegoResults(t *testing.T) {
	t.Run("empty results default to allowed", func(t *testing.T) {
		result, err := parseRegoResults(nil)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Allowed {
			t.Error("empty results should default to allowed")
		}
	})
}

func TestExtractStringList(t *testing.T) {
	t.Run("string slice", func(t *testing.T) {
		input := []interface{}{"a", "b", "c"}
		result := extractStringList(input)
		if len(result) != 3 {
			t.Errorf("len = %d, want 3", len(result))
		}
	})

	t.Run("single string", func(t *testing.T) {
		result := extractStringList("hello")
		if len(result) != 1 || result[0] != "hello" {
			t.Errorf("result = %v, want [hello]", result)
		}
	})
}

func TestDiscoverRegoFile(t *testing.T) {
	// Create temp dir with policy
	dir := t.TempDir()
	policyDir := dir + "/.ods"
	os.MkdirAll(policyDir, 0755)
	os.WriteFile(policyDir+"/policy.rego", []byte("package ods.policy"), 0644)

	path := DiscoverRegoFile(dir)
	if path == "" {
		t.Error("DiscoverRegoFile should find .ods/policy.rego")
	}
	if !strings.HasSuffix(path, ".ods/policy.rego") {
		t.Errorf("path = %s, want ending with .ods/policy.rego", path)
	}
}

func TestDefaultRegoPolicy(t *testing.T) {
	policy := DefaultRegoPolicy()
	if !strings.Contains(policy, "package ods.policy") {
		t.Error("default policy missing package declaration")
	}
	if !strings.Contains(policy, "deny") {
		t.Error("default policy missing deny rules")
	}
}

// writeTempPolicy writes a policy string to a temp file and returns its path.
func writeTempPolicy(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "ods-tier-*.rego")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

const tierPolicy = `package ods.policy

default allow := true
default review_tier := "standard"

review_tier := "auto" {
    input.technical_debt_delta <= 1.0
    not has_high_or_critical
}

review_tier := "elevated" {
    input.ai_generated == true
    has_high_or_critical
}

has_high_or_critical {
    input.issues[_].severity == "critical"
}

has_high_or_critical {
    input.issues[_].severity == "high"
}
`

func TestReviewTierRouting(t *testing.T) {
	path := writeTempPolicy(t, tierPolicy)

	t.Run("low risk routes to auto", func(t *testing.T) {
		result, err := Evaluate(path, &EvalInput{TechnicalDebtDelta: 0.5})
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if result.ReviewTier != ReviewTierAuto {
			t.Errorf("review_tier = %q, want %q", result.ReviewTier, ReviewTierAuto)
		}
	})

	t.Run("AI change with high issue routes to elevated", func(t *testing.T) {
		result, err := Evaluate(path, &EvalInput{
			AIGenerated:        true,
			TechnicalDebtDelta: 2.0,
			Issues:             []EvalIssue{{Rule: "x", Severity: "high"}},
		})
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if result.ReviewTier != ReviewTierElevated {
			t.Errorf("review_tier = %q, want %q", result.ReviewTier, ReviewTierElevated)
		}
	})

	t.Run("everything else routes to standard", func(t *testing.T) {
		result, err := Evaluate(path, &EvalInput{TechnicalDebtDelta: 2.5})
		if err != nil {
			t.Fatalf("evaluate failed: %v", err)
		}
		if result.ReviewTier != ReviewTierStandard {
			t.Errorf("review_tier = %q, want %q", result.ReviewTier, ReviewTierStandard)
		}
	})
}

func TestReviewTierUnknownValueFallsBack(t *testing.T) {
	path := writeTempPolicy(t, `package ods.policy

default allow := true
review_tier := "yolo"
`)
	result, err := Evaluate(path, &EvalInput{})
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	if result.ReviewTier != ReviewTierStandard {
		t.Errorf("review_tier = %q, want fallback %q", result.ReviewTier, ReviewTierStandard)
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "unknown review_tier") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an unknown-review_tier warning, got %v", result.Warnings)
	}
}

func TestReviewTierAbsentStaysEmpty(t *testing.T) {
	// A policy that defines no review_tier at all (the default policy does).
	path := writeTempPolicy(t, "package ods.policy\n\ndefault allow := true\n")
	result, err := Evaluate(path, &EvalInput{})
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	if result.ReviewTier != "" {
		t.Errorf("review_tier = %q, want empty (rule not defined)", result.ReviewTier)
	}
}

// ── AI reviewer verdicts (input.ai_reviews) ──────────────────────────────────

func TestDefaultPolicyAIReviewRoutesNotDenies(t *testing.T) {
	path := writeTempPolicy(t, DefaultRegoPolicy())

	in := &EvalInput{
		AIReviews: []EvalAIReview{{
			Tool:    "claude-code",
			Verdict: "request_changes",
			Findings: []EvalReviewIssue{
				{File: "a.go", Line: 1, Severity: "high", Message: "logic error"},
			},
		}},
	}
	res, err := Evaluate(path, in)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	// Core contract: probabilistic opinions tighten, never deny.
	if !res.Allowed {
		t.Errorf("AI review must not deny by default, got denials: %v", res.Denials)
	}
	if res.ReviewTier != ReviewTierElevated {
		t.Errorf("review_tier = %q, want elevated on request_changes", res.ReviewTier)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "claude-code") && strings.Contains(w, "requested changes") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a request-changes warning, got %v", res.Warnings)
	}
}

func TestDefaultPolicyUndisclosedAIWarns(t *testing.T) {
	path := writeTempPolicy(t, DefaultRegoPolicy())

	cases := []struct {
		name     string
		in       *EvalInput
		wantWarn bool
	}{
		{
			"suspected AI without disclosure warns",
			&EvalInput{AIGenerated: true, AIConfidence: 0.6,
				DetectionSources: []string{"branch-name", "diff-heuristics"}},
			true,
		},
		{
			"trailer disclosure silences the nudge",
			&EvalInput{AIGenerated: true, AIConfidence: 0.9,
				DetectionSources: []string{"commit-trailer", "branch-name"}},
			false,
		},
		{
			"pr-body disclosure silences the nudge",
			&EvalInput{AIGenerated: true, AIConfidence: 0.8,
				DetectionSources: []string{"pr-body"}},
			false,
		},
		{
			"git-ai notes count as disclosure",
			&EvalInput{AIGenerated: true, AIConfidence: 0.9,
				DetectionSources: []string{"git-ai-notes"}},
			false,
		},
		{
			"human code never nagged",
			&EvalInput{AIGenerated: false},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Evaluate(path, tc.in)
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if !res.Allowed {
				t.Errorf("disclosure nudge must never deny, got denials: %v", res.Denials)
			}
			got := false
			for _, w := range res.Warnings {
				if strings.Contains(w, "without author disclosure") {
					got = true
				}
			}
			if got != tc.wantWarn {
				t.Errorf("disclosure warning = %v, want %v (warnings: %v)", got, tc.wantWarn, res.Warnings)
			}
		})
	}
}

func TestDefaultPolicyMergeConfidence(t *testing.T) {
	path := writeTempPolicy(t, DefaultRegoPolicy())

	cases := []struct {
		name     string
		in       *EvalInput
		wantWarn string // substring expected in warnings ("" = none of ours)
		wantTier string // the default policy routes a clean input to "auto"
	}{
		{
			"AI source without tests warns and elevates",
			&EvalInput{AIGenerated: true, DetectionSources: []string{"commit-trailer"}, PatchCoverage: -1, MutationScore: -1,
				MergeConfidence: &EvalMergeConfidence{AddedSourceWithoutTests: true, SourceFilesChanged: 1}},
			"no tests", ReviewTierElevated,
		},
		{
			"human source without tests warns but does not elevate",
			&EvalInput{AIGenerated: false,
				MergeConfidence: &EvalMergeConfidence{AddedSourceWithoutTests: true}},
			"no tests", ReviewTierAuto,
		},
		{
			"AI change touching a risky path warns and elevates",
			&EvalInput{AIGenerated: true, DetectionSources: []string{"commit-trailer"}, PatchCoverage: -1, MutationScore: -1,
				MergeConfidence: &EvalMergeConfidence{RiskyPaths: []string{".github/workflows/ci.yml"}}},
			"sensitive path", ReviewTierElevated,
		},
		{
			"tested AI change is neutral",
			&EvalInput{AIGenerated: true, DetectionSources: []string{"commit-trailer"}, PatchCoverage: -1, MutationScore: -1,
				MergeConfidence: &EvalMergeConfidence{TestsTouched: true, SourceFilesChanged: 1, TestFilesChanged: 1}},
			"", ReviewTierAuto,
		},
		{
			"absent merge_confidence is safe",
			&EvalInput{AIGenerated: true, DetectionSources: []string{"commit-trailer"}, PatchCoverage: -1, MutationScore: -1},
			"", ReviewTierAuto,
		},
		{
			"AI change with low patch coverage warns and elevates",
			&EvalInput{AIGenerated: true, DetectionSources: []string{"commit-trailer"}, PatchCoverage: 0.4, MutationScore: -1},
			"added lines are covered", ReviewTierElevated,
		},
		{
			"AI change with full patch coverage is neutral",
			&EvalInput{AIGenerated: true, DetectionSources: []string{"commit-trailer"}, PatchCoverage: 1.0, MutationScore: -1},
			"", ReviewTierAuto,
		},
		{
			"human low patch coverage neither warns nor elevates",
			&EvalInput{AIGenerated: false, PatchCoverage: 0.1},
			"", ReviewTierAuto,
		},
		{
			"unmeasured patch coverage does not fire",
			&EvalInput{AIGenerated: true, DetectionSources: []string{"commit-trailer"}, PatchCoverage: -1, MutationScore: -1},
			"", ReviewTierAuto,
		},
		{
			"AI change with weak mutation score warns and elevates",
			&EvalInput{AIGenerated: true, DetectionSources: []string{"commit-trailer"}, PatchCoverage: -1, MutationScore: 0.3},
			"kill only", ReviewTierElevated,
		},
		{
			"AI change with strong mutation score is neutral",
			&EvalInput{AIGenerated: true, DetectionSources: []string{"commit-trailer"}, PatchCoverage: -1, MutationScore: 0.9},
			"", ReviewTierAuto,
		},
		{
			"human weak mutation score neither warns nor elevates",
			&EvalInput{AIGenerated: false, PatchCoverage: -1, MutationScore: 0.1},
			"", ReviewTierAuto,
		},
		{
			"unmeasured mutation score does not fire",
			&EvalInput{AIGenerated: true, DetectionSources: []string{"commit-trailer"}, PatchCoverage: -1, MutationScore: -1},
			"", ReviewTierAuto,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Evaluate(path, tc.in)
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if !res.Allowed {
				t.Errorf("merge-confidence signals must never deny by default, got: %v", res.Denials)
			}
			if res.ReviewTier != tc.wantTier {
				t.Errorf("review_tier = %q, want %q (warnings: %v)", res.ReviewTier, tc.wantTier, res.Warnings)
			}
			if tc.wantWarn != "" {
				found := false
				for _, w := range res.Warnings {
					if strings.Contains(w, tc.wantWarn) {
						found = true
					}
				}
				if !found {
					t.Errorf("expected a warning containing %q, got %v", tc.wantWarn, res.Warnings)
				}
			}
		})
	}
}

func TestDefaultPolicyAIReviewApproveIsNeutral(t *testing.T) {
	path := writeTempPolicy(t, DefaultRegoPolicy())
	in := &EvalInput{
		AIReviews: []EvalAIReview{{Tool: "coderabbit", Verdict: "approve"}},
	}
	res, err := Evaluate(path, in)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	// An approve must not loosen anything: no tier change, no warnings from it.
	if !res.Allowed {
		t.Errorf("approve should not deny: %v", res.Denials)
	}
	baseline, err := Evaluate(path, &EvalInput{})
	if err != nil {
		t.Fatalf("evaluate baseline: %v", err)
	}
	if res.ReviewTier != baseline.ReviewTier {
		t.Errorf("an AI approve changed the tier: %q, without the review %q", res.ReviewTier, baseline.ReviewTier)
	}
}

func TestOptInDenyOverAIReviews(t *testing.T) {
	// Teams can explicitly opt in to enforcement over ai_reviews.
	path := writeTempPolicy(t, `package ods.policy

default allow := true

deny[msg] {
    f := input.ai_reviews[_].findings[_]
    f.severity == "high"
    msg = sprintf("AI review high finding: %s", [f.message])
}
`)
	in := &EvalInput{
		AIReviews: []EvalAIReview{{
			Tool: "x", Verdict: "request_changes",
			Findings: []EvalReviewIssue{{Severity: "high", Message: "boom"}},
		}},
	}
	res, err := Evaluate(path, in)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Allowed {
		t.Error("explicit opt-in deny over ai_reviews must be able to block")
	}
}

// TestEvidenceTierReachesPolicy locks in that input.evidence_tier marshals
// through EvalInput to Rego (the conformance suite covers this too, but only
// runs when ODS_CONFORMANCE_DIR is set; this keeps it in the hermetic suite).
func TestEvidenceTierReachesPolicy(t *testing.T) {
	policy := `package ods.policy
default allow := true
default review_tier := "standard"
strong_evidence { input.evidence_tier == "corroborated" }
strong_evidence { input.evidence_tier == "attested" }
review_tier := "elevated" {
    input.ai_generated
    not strong_evidence
}`
	path := writeTempPolicy(t, policy)
	cases := []struct {
		name string
		tier string
		want string
	}{
		{"attested stays standard", "attested", "standard"},
		{"corroborated stays standard", "corroborated", "standard"},
		{"inferred routes elevated", "inferred", ReviewTierElevated},
		{"empty tier routes elevated", "", ReviewTierElevated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Evaluate(path, &EvalInput{
				AIGenerated:      true,
				DetectionSources: []string{"x"},
				EvidenceTier:     tc.tier,
			})
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if res.ReviewTier != tc.want {
				t.Errorf("review_tier = %q, want %q", res.ReviewTier, tc.want)
			}
		})
	}
}

// ── Evaluate: failure modes ──────────────────────────────────────────────────

// TestEvaluate_UnreadablePolicy guards the policy-file read failure: a missing
// file, or a path that is not a file, is an error that names the path and keeps
// the underlying cause, and produces no verdict.
func TestEvaluate_UnreadablePolicy(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.rego")

	res, err := Evaluate(missing, &EvalInput{})
	if res != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Evaluate(missing) = (%v, %v), want (nil, an os.ErrNotExist error)", res, err)
	}
	if !strings.Contains(err.Error(), "reading policy file") || !strings.Contains(err.Error(), missing) {
		t.Errorf("error = %q, want it to say which policy file could not be read", err)
	}

	res, err = Evaluate(dir, &EvalInput{})
	if res != nil || err == nil || !strings.Contains(err.Error(), "reading policy file") {
		t.Errorf("Evaluate(directory) = (%v, %v), want a reading-policy-file error", res, err)
	}
}

// TestEvaluate_UnmarshalableInput guards the input conversion: values JSON
// cannot represent (NaN, infinity), at the top level or nested in a list, fail
// before the policy runs instead of being passed to it in some altered form.
func TestEvaluate_UnmarshalableInput(t *testing.T) {
	path := writeTempPolicy(t, "package ods.policy\n\ndefault allow := true\n")
	cases := map[string]*EvalInput{
		"NaN confidence":      {AIConfidence: math.NaN()},
		"infinite debt":       {TechnicalDebtDelta: math.Inf(1)},
		"NaN in a file entry": {AIFiles: []EvalFileInfo{{Path: "a.go", Confidence: math.NaN()}}},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := Evaluate(path, in)
			if res != nil || err == nil || !strings.Contains(err.Error(), "marshaling input") {
				t.Errorf("Evaluate = (%v, %v), want a marshaling-input error", res, err)
			}
		})
	}
}

// TestEvaluate_PolicyErrors guards that a broken policy is an error, never a
// silent allow: compile-time failures (syntax, unsafe variables) surface as a
// preparation error, and rules that contradict each other for the given input
// surface as an evaluation error.
func TestEvaluate_PolicyErrors(t *testing.T) {
	cases := []struct {
		name    string
		policy  string
		wantErr string
	}{
		{"syntax error", "package ods.policy\n\ndeny[msg] {\n", "preparing policy"},
		{"unsafe variable", "package ods.policy\n\ndeny[msg] { true }\n", "preparing policy"},
		{
			"conflicting complete rules",
			"package ods.policy\n\nreview_tier := \"auto\"\nreview_tier := \"elevated\"\n",
			"evaluating policy",
		},
		{"contradicting verdicts", "package ods.policy\n\nallow := true\nallow := false\n", "evaluating policy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := Evaluate(writeTempPolicy(t, c.policy), &EvalInput{})
			if res != nil || err == nil {
				t.Fatalf("Evaluate = (%v, %v), want an error and no verdict", res, err)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

// ── Evaluate: verdict shapes ─────────────────────────────────────────────────

// TestEvaluate_ObjectAndStringVerdicts guards that deny and warn may be written
// as keyed objects or plain strings, not only sets: object values and a bare
// string each become messages, and a denial blocks the change.
func TestEvaluate_ObjectAndStringVerdicts(t *testing.T) {
	path := writeTempPolicy(t, `package ods.policy

default allow := true

deny[key] = msg {
    key := "needs-tests"
    msg := "tests are required"
}

deny[key] = msg {
    key := "needs-owner"
    msg := "an owner is required"
}

warn := "just one warning"
`)
	res, err := Evaluate(path, &EvalInput{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Allowed {
		t.Error("a denial must block the change")
	}
	denials := append([]string(nil), res.Denials...)
	sort.Strings(denials)
	if want := []string{"an owner is required", "tests are required"}; !reflect.DeepEqual(denials, want) {
		t.Errorf("denials = %v, want %v (the object's values)", denials, want)
	}
	if want := []string{"just one warning"}; !reflect.DeepEqual(res.Warnings, want) {
		t.Errorf("warnings = %v, want %v", res.Warnings, want)
	}
}

// TestEvaluate_ExplicitAllow guards the allow rule: a policy can refuse a
// change without any denial message by deriving allow = false, and otherwise
// the default applies.
func TestEvaluate_ExplicitAllow(t *testing.T) {
	path := writeTempPolicy(t, `package ods.policy

default allow := true

allow := false {
    input.ai_generated
}
`)
	for _, ai := range []bool{true, false} {
		res, err := Evaluate(path, &EvalInput{AIGenerated: ai})
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if res.Allowed == ai || len(res.Denials) != 0 {
			t.Errorf("ai_generated=%v: allowed=%v denials=%v, want allowed=%v with no denials", ai, res.Allowed, res.Denials, !ai)
		}
	}
}

// ── parseRegoResults ─────────────────────────────────────────────────────────

// TestParseRegoResults_Shapes guards how a Rego result set is read, using
// hand-built results: which binding holds the policy document, what is done when
// none does, and how each verdict field (deny, warn, allow, review_tier) is read.
func TestParseRegoResults_Shapes(t *testing.T) {
	bind := func(b rego.Vars) rego.ResultSet { return rego.ResultSet{{Bindings: b}} }
	list := func(items ...interface{}) []interface{} { return items }
	doc := func(k string, v interface{}) map[string]interface{} { return map[string]interface{}{k: v} }

	cases := []struct {
		name string
		rs   rego.ResultSet
		want EvalResult
	}{
		// Locating the policy document.
		{"no results", rego.ResultSet{}, EvalResult{Allowed: true}},
		{"a result without bindings", rego.ResultSet{{}}, EvalResult{Allowed: true}},
		{"bindings holding no object", bind(rego.Vars{"result": "text", "n": 3.0}), EvalResult{Allowed: true}},
		{"the result binding", bind(rego.Vars{"result": doc("deny", list("no"))}), EvalResult{Denials: []string{"no"}}},
		{
			"the x binding when result is absent",
			bind(rego.Vars{"x": doc("warn", list("w")), "other": doc("deny", list("ignored"))}),
			EvalResult{Allowed: true, Warnings: []string{"w"}},
		},
		{
			"the data.ods.policy binding when the others are absent",
			bind(rego.Vars{"data.ods.policy": doc("warn", list("w"))}),
			EvalResult{Allowed: true, Warnings: []string{"w"}},
		},
		{
			"result wins over x",
			bind(rego.Vars{"result": doc("warn", list("from result")), "x": doc("warn", list("from x"))}),
			EvalResult{Allowed: true, Warnings: []string{"from result"}},
		},
		{
			"a result that is not an object falls through to x",
			bind(rego.Vars{"result": "text", "x": doc("warn", list("from x"))}),
			EvalResult{Allowed: true, Warnings: []string{"from x"}},
		},
		{
			"any other object binding is the last resort",
			bind(rego.Vars{"whatever": doc("deny", list("no"))}),
			EvalResult{Denials: []string{"no"}},
		},

		// Reading deny and warn.
		{"deny as a single string", bind(rego.Vars{"result": doc("deny", "single")}), EvalResult{Denials: []string{"single"}}},
		{
			"deny as an object uses its values",
			bind(rego.Vars{"result": doc("deny", map[string]interface{}{"k": "reason"})}),
			EvalResult{Denials: []string{"reason"}},
		},
		{
			"non-string entries are dropped",
			bind(rego.Vars{"result": doc("deny", list("a", 1.0, nil, "b"))}),
			EvalResult{Denials: []string{"a", "b"}},
		},
		{"deny of an unsupported type is ignored", bind(rego.Vars{"result": doc("deny", 5.0)}), EvalResult{Allowed: true}},
		{"an empty deny set denies nothing", bind(rego.Vars{"result": doc("deny", list())}), EvalResult{Allowed: true}},

		// Reading allow.
		{"allow false without a denial", bind(rego.Vars{"result": doc("allow", false)}), EvalResult{}},
		{"allow true", bind(rego.Vars{"result": doc("allow", true)}), EvalResult{Allowed: true}},
		{"a non-boolean allow is ignored", bind(rego.Vars{"result": doc("allow", "no")}), EvalResult{Allowed: true}},
		{
			"a denial overrides allow true",
			bind(rego.Vars{"result": map[string]interface{}{"allow": true, "deny": list("d")}}),
			EvalResult{Denials: []string{"d"}},
		},

		// Reading review_tier.
		{"tier auto", bind(rego.Vars{"result": doc("review_tier", "auto")}), EvalResult{Allowed: true, ReviewTier: "auto"}},
		{"tier standard", bind(rego.Vars{"result": doc("review_tier", "standard")}), EvalResult{Allowed: true, ReviewTier: "standard"}},
		{"tier elevated", bind(rego.Vars{"result": doc("review_tier", "elevated")}), EvalResult{Allowed: true, ReviewTier: "elevated"}},
		{"a non-string tier is ignored", bind(rego.Vars{"result": doc("review_tier", 3.0)}), EvalResult{Allowed: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseRegoResults(c.rs)
			if err != nil {
				t.Fatalf("parseRegoResults: %v", err)
			}
			if !reflect.DeepEqual(*got, c.want) {
				t.Errorf("result = %+v, want %+v", *got, c.want)
			}
		})
	}
}

// TestParseRegoResults_UnknownTierWarning guards the routing fallback: an
// unrecognized review_tier becomes "standard" and adds a warning naming the bad
// value, after any warnings the policy itself produced.
func TestParseRegoResults_UnknownTierWarning(t *testing.T) {
	rs := rego.ResultSet{{Bindings: rego.Vars{"result": map[string]interface{}{
		"warn":        []interface{}{"from the policy"},
		"review_tier": "yolo",
	}}}}
	got, err := parseRegoResults(rs)
	if err != nil {
		t.Fatalf("parseRegoResults: %v", err)
	}
	if !got.Allowed || got.ReviewTier != ReviewTierStandard {
		t.Errorf("allowed=%v tier=%q, want allowed with tier %q", got.Allowed, got.ReviewTier, ReviewTierStandard)
	}
	if len(got.Warnings) != 2 || got.Warnings[0] != "from the policy" {
		t.Fatalf("warnings = %v, want the policy's warning followed by the fallback notice", got.Warnings)
	}
	if w := got.Warnings[1]; !strings.Contains(w, `"yolo"`) || !strings.Contains(w, "unknown review_tier") {
		t.Errorf("fallback warning = %q, want it to name the unknown tier", w)
	}
}

// ── extractStringList ────────────────────────────────────────────────────────

// TestExtractStringList_Types guards the conversion of every result shape: lists
// and objects keep their string entries, a bare string is one message, and any
// other type yields nothing. Object values have no defined order, so they are
// compared sorted.
func TestExtractStringList_Types(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		want []string
	}{
		{"list", []interface{}{"a", "b"}, []string{"a", "b"}},
		{"list drops non-strings", []interface{}{"a", 1.0, nil, true, map[string]interface{}{}, "b"}, []string{"a", "b"}},
		{"empty list", []interface{}{}, nil},
		{"object values", map[string]interface{}{"k1": "v1", "k2": "v2"}, []string{"v1", "v2"}},
		{"object drops non-strings", map[string]interface{}{"k1": "v1", "k2": 2.0}, []string{"v1"}},
		{"empty object", map[string]interface{}{}, nil},
		{"single string", "msg", []string{"msg"}},
		{"number", 3.0, nil},
		{"boolean", true, nil},
		{"nil", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractStringList(c.in)
			sort.Strings(got)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("extractStringList(%#v) = %#v, want %#v", c.in, got, c.want)
			}
		})
	}
}

// ── DiscoverRegoFile ─────────────────────────────────────────────────────────

// TestDiscoverRegoFile_Candidates guards the search order for a repository's
// policy: .ods/policy.rego, then .ods.rego, then policy.rego at the root; the
// first that exists wins, other Rego files are not policies, and a repository
// with none yields "".
func TestDiscoverRegoFile_Candidates(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  string // relative to the repository root; "" = none found
	}{
		{"hidden directory policy", []string{".ods/policy.rego"}, ".ods/policy.rego"},
		{"hidden file policy", []string{".ods.rego"}, ".ods.rego"},
		{"root policy", []string{"policy.rego"}, "policy.rego"},
		{"hidden directory beats the others", []string{"policy.rego", ".ods.rego", ".ods/policy.rego"}, ".ods/policy.rego"},
		{"hidden file beats the root policy", []string{"policy.rego", ".ods.rego"}, ".ods.rego"},
		{"other Rego files are not policies", []string{"other.rego", ".ods/other.rego"}, ""},
		{"empty repository", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range c.files {
				path := filepath.Join(dir, f)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("package ods.policy\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want := ""
			if c.want != "" {
				want = filepath.Join(dir, c.want)
			}
			if got := DiscoverRegoFile(dir); got != want {
				t.Errorf("DiscoverRegoFile = %q, want %q", got, want)
			}
		})
	}

	t.Run("repository root that does not exist", func(t *testing.T) {
		if got := DiscoverRegoFile(filepath.Join(t.TempDir(), "absent")); got != "" {
			t.Errorf("DiscoverRegoFile = %q, want none", got)
		}
	})
}
