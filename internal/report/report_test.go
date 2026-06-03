package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-delivery-spec/cli/internal/policy"
)

const validCommitMessage = `feat(auth): add oauth login

AI-assisted: true
AI-tool: GitHub Copilot
AI-scope: auth module`

const validPRBody = `## Summary
Add OAuth login.

## Type
- [x] Feature

## AI Disclosure
- [x] This PR contains AI-generated code
- AI Tool: GitHub Copilot

## Changes
- Added provider integration.

## Testing
- Unit tests added.

## Checklist
- [x] Branch follows ODS.`

func TestAllChecksRunsAll10(t *testing.T) {
	chk := AllChecks(CheckInputs{
		BranchName:    "feature/add-oauth-login",
		CommitMessage: validCommitMessage,
		PRBody:        validPRBody,
		ChangedFiles:  []string{"auth.go", "auth_test.go"},
	}, Options{}, nil)

	if len(chk) != 10 {
		t.Fatalf("expected 10 checks, got %d", len(chk))
	}
}

func TestAIDisclosureCheck(t *testing.T) {
	// Policy requires AI disclosure (enterprise default)
	p := &policy.Policy{
		AIDisclosure: policy.AIDisclosure{Required: true, StrictToolName: true},
		SeverityMap: map[string]policy.Severity{
			"pr_ai_tool": policy.SeverityError,
		},
	}

	// Case 1: Full AI disclosure with tool → pass
	c := checkAIDisclosure(CheckInputs{
		CommitMessage: validCommitMessage,
		PRBody:        validPRBody,
	}, p)
	if c.Status != CheckPass {
		t.Fatalf("expected pass, got %s: %v", c.Status, c.Errors)
	}
	if c.Score != 10 {
		t.Fatalf("expected score 10, got %d", c.Score)
	}

	// Case 2: No disclosure, policy requires it → fail
	c = checkAIDisclosure(CheckInputs{
		CommitMessage: "feat(auth): add login",
		PRBody:        "## Summary\nNo AI disclosure here.",
	}, p)
	if c.Status != CheckFail {
		t.Fatalf("expected fail, got %s: %v", c.Status, c.Errors)
	}

	// Case 3: AI disclosure without tool → fail
	c = checkAIDisclosure(CheckInputs{
		CommitMessage: "feat(auth): add login\n\nAI-assisted: true",
		PRBody:        "## AI Disclosure\n- [x] This PR contains AI-generated code",
	}, p)
	if c.Status != CheckFail {
		t.Fatalf("expected fail for missing tool, got %s", c.Status)
	}

	// Case 4: Co-authored-by AI trailer → pass
	c = checkAIDisclosure(CheckInputs{
		CommitMessage: "feat(auth): add login\n\nCo-authored-by: GitHub Copilot <copilot@github.com>\nAI-tool: GitHub Copilot",
	}, p)
	if c.Status != CheckPass {
		t.Fatalf("expected pass for co-authored-by, got %s: %v", c.Status, c.Errors)
	}

	// Case 5: No policy → passes even without disclosure
	c = checkAIDisclosure(CheckInputs{
		CommitMessage: "feat(auth): add login",
	}, &policy.Policy{AIDisclosure: policy.AIDisclosure{Required: false}})
	if c.Status != CheckPass {
		t.Fatalf("expected pass when disclosure not required, got %s", c.Status)
	}
}

func TestHumanReviewEvidenceCheck(t *testing.T) {
	// No review data → skipped
	c := checkHumanReviewEvidence(CheckInputs{})
	if c.Status != CheckSkipped {
		t.Fatalf("expected skipped without data, got %s", c.Status)
	}

	// Human review detected
	c = checkHumanReviewEvidence(CheckInputs{
		ReviewerLogins: []string{"jane-doe", "github-actions[bot]"},
		PRAuthor:       "alice-smith",
	})
	if c.Status != CheckPass {
		t.Fatalf("expected pass with human review, got %s", c.Status)
	}
	if c.Score != 10 {
		t.Fatalf("expected score 10, got %d", c.Score)
	}

	// Only bot reviews → fail
	c = checkHumanReviewEvidence(CheckInputs{
		ReviewerLogins: []string{"dependabot[bot]", "github-actions[bot]"},
		PRAuthor:       "alice-smith",
	})
	if c.Status != CheckFail {
		t.Fatalf("expected fail with only bot reviews, got %s", c.Status)
	}

	// Self-approve → fail
	c = checkHumanReviewEvidence(CheckInputs{
		ReviewerLogins: []string{"alice-smith"},
		PRAuthor:       "alice-smith",
	})
	if c.Status != CheckFail {
		t.Fatalf("expected fail for self-approve, got %s", c.Status)
	}

	// AI agent PR with no human review → CRITICAL failure
	c = checkHumanReviewEvidence(CheckInputs{
		ReviewerLogins: []string{},
		PRAuthor:       "github-actions[bot]",
	})
	if c.Status != CheckFail {
		t.Fatalf("expected fail for AI agent PR with no review, got %s", c.Status)
	}
	if len(c.Errors) == 0 || !strings.Contains(c.Errors[0], "CRITICAL") {
		t.Fatalf("expected CRITICAL error for AI agent PR, got: %v", c.Errors)
	}

	// AI agent PR with human review → pass with note
	c = checkHumanReviewEvidence(CheckInputs{
		ReviewerLogins: []string{"jane-doe", "copilot-pull-request-reviewer"},
		PRAuthor:       "github-actions[bot]",
	})
	if c.Status != CheckPass {
		t.Fatalf("expected pass for AI agent PR with human review, got %s: %v", c.Status, c.Errors)
	}
	foundAIMsg := false
	for _, n := range c.Notes {
		if strings.Contains(n, "AI agent PR") {
			foundAIMsg = true
		}
	}
	if !foundAIMsg {
		t.Fatal("expected note about AI agent PR having human oversight")
	}
}

func TestRequiredCICheck(t *testing.T) {
	// No CI → fail
	c := checkRequiredCI(CheckInputs{CIWorkflowsExist: false})
	if c.Status != CheckFail {
		t.Fatalf("expected fail without CI, got %s", c.Status)
	}

	// CI with PR trigger → pass
	c = checkRequiredCI(CheckInputs{
		CIWorkflowsExist:    true,
		CIWorkflowContent:   "on:\n  pull_request:\n    types: [opened]",
	})
	if c.Status != CheckPass {
		t.Fatalf("expected pass with CI + PR trigger, got %s", c.Status)
	}
}

func TestApprovalPolicyCheck(t *testing.T) {
	// No protection → fail
	c := checkApprovalPolicy(CheckInputs{})
	if c.Status != CheckFail {
		t.Fatalf("expected fail without protection, got %s", c.Status)
	}

	// Branch protection enabled → pass
	c = checkApprovalPolicy(CheckInputs{
		BranchProtectionEnabled: true,
		RequiredApprovals:       2,
	})
	if c.Status != CheckPass {
		t.Fatalf("expected pass with branch protection, got %s", c.Status)
	}
}

func TestAIAgentCommitDetectionCheck(t *testing.T) {
	// No email → skipped
	c := checkAIAgentCommitDetection(CheckInputs{})
	if c.Status != CheckSkipped {
		t.Fatalf("expected skipped without email, got %s", c.Status)
	}

	// Normal email → pass
	c = checkAIAgentCommitDetection(CheckInputs{
		CommitAuthorEmail: "human@example.com",
	})
	if c.Status != CheckPass {
		t.Fatalf("expected pass for human email, got %s", c.Status)
	}

	// AI agent email → warning
	c = checkAIAgentCommitDetection(CheckInputs{
		CommitAuthorEmail: "bot+ai-agent@openai.com",
	})
	if c.Status != CheckWarning {
		t.Fatalf("expected warning for AI agent, got %s", c.Status)
	}
}

func TestTestEvidenceCheck(t *testing.T) {
	// No changed files → skipped
	c := checkTestEvidence(CheckInputs{})
	if c.Status != CheckSkipped {
		t.Fatalf("expected skipped without files, got %s", c.Status)
	}

	// Has test files → pass
	c = checkTestEvidence(CheckInputs{
		ChangedFiles: []string{"auth.go", "auth_test.go", "handler.go"},
	})
	if c.Status != CheckPass {
		t.Fatalf("expected pass with test files, got %s", c.Status)
	}

	// No test files → warning
	c = checkTestEvidence(CheckInputs{
		ChangedFiles: []string{"auth.go", "handler.go", "utils.go"},
	})
	if c.Status != CheckWarning {
		t.Fatalf("expected warning without test files, got %s", c.Status)
	}
}

func TestSecurityScanEvidenceCheck(t *testing.T) {
	// No CI content → skipped
	c := checkSecurityScanEvidence(CheckInputs{})
	if c.Status != CheckSkipped {
		t.Fatalf("expected skipped without CI content, got %s", c.Status)
	}

	// Has security tool → pass
	c = checkSecurityScanEvidence(CheckInputs{
		CIWorkflowContent: "steps:\n  - uses: github/codeql-action/analyze@v3",
	})
	if c.Status != CheckPass {
		t.Fatalf("expected pass with security tool, got %s", c.Status)
	}

	// No security tool → warning
	c = checkSecurityScanEvidence(CheckInputs{
		CIWorkflowContent: "steps:\n  - run: go test ./...",
	})
	if c.Status != CheckWarning {
		t.Fatalf("expected warning without security tool, got %s", c.Status)
	}
}

func TestReleaseReadinessCheck(t *testing.T) {
	c := checkReleaseReadiness(CheckInputs{ReleaseHasODSCheck: false})
	if c.Status != CheckWarning {
		t.Fatalf("expected warning without ODS release check, got %s", c.Status)
	}

	c = checkReleaseReadiness(CheckInputs{ReleaseHasODSCheck: true})
	if c.Status != CheckPass {
		t.Fatalf("expected pass with ODS release check, got %s", c.Status)
	}
}

func TestSummarizeWeightedScoring(t *testing.T) {
	// All checks pass
	allPass := func() []Check {
		return []Check{
			{ID: "ai-disclosure", Name: "AI Disclosure", Status: CheckPass, Score: 10, Weight: 10},
			{ID: "human-review-evidence", Name: "Human Review", Status: CheckPass, Score: 10, Weight: 10},
			{ID: "required-ci", Name: "Required CI", Status: CheckPass, Score: 7, Weight: 7},
			{ID: "commit-message", Name: "Commit Message", Status: CheckPass, Score: 2, Weight: 2},
		}
	}
	score, status := summarize(allPass())
	if status != StatusCompliant {
		t.Fatalf("expected compliant, got %s", status)
	}
	if score != 100 {
		t.Fatalf("expected score 100, got %d", score)
	}

	// One critical failure
	withFailure := func() []Check {
		return []Check{
			{ID: "ai-disclosure", Name: "AI Disclosure", Status: CheckFail, Score: 0, Weight: 10},
			{ID: "human-review-evidence", Name: "Human Review", Status: CheckPass, Score: 10, Weight: 10},
			{ID: "required-ci", Name: "Required CI", Status: CheckPass, Score: 7, Weight: 7},
			{ID: "commit-message", Name: "Commit Message", Status: CheckPass, Score: 2, Weight: 2},
		}
	}
	score, status = summarize(withFailure())
	if status != StatusNonCompliant {
		t.Fatalf("expected non_compliant, got %s", status)
	}
	// totalScore=19, maxScore=29 → 19*100/29 = 65
	if score != 65 {
		t.Fatalf("expected score 65, got %d", score)
	}

	// Half warnings
	withWarnings := func() []Check {
		return []Check{
			{ID: "ai-disclosure", Name: "AI Disclosure", Status: CheckWarning, Score: 5, Weight: 10},
			{ID: "human-review-evidence", Name: "Human Review", Status: CheckPass, Score: 10, Weight: 10},
		}
	}
	score, status = summarize(withWarnings())
	if status != StatusCompliantWithWarnings {
		t.Fatalf("expected compliant_with_warnings, got %s", status)
	}
	// totalScore=15, maxScore=20 → 15*100/20 = 75
	if score != 75 {
		t.Fatalf("expected score 75, got %d", score)
	}

	// Skipped checks excluded from scoring
	withSkipped := func() []Check {
		return []Check{
			{ID: "ai-disclosure", Name: "AI Disclosure", Status: CheckPass, Score: 10, Weight: 10},
			{ID: "human-review-evidence", Name: "Human Review", Status: CheckSkipped, Weight: 10},
			{ID: "commit-message", Name: "Commit Message", Status: CheckPass, Score: 2, Weight: 2},
		}
	}
	score, status = summarize(withSkipped())
	if status != StatusCompliant {
		t.Fatalf("expected compliant with skipped, got %s", status)
	}
	// totalScore=12, maxScore=12 → 100
	if score != 100 {
		t.Fatalf("expected score 100 with skipped excluded, got %d", score)
	}
}

func TestBuildReportIntegratesAllChecks(t *testing.T) {
	report := Build(Inputs{
		BranchName:    "feature/add-oauth-login",
		CommitMessage: validCommitMessage,
		PRBody:        validPRBody,
		Repository:    "open-delivery-spec/example",
		Ref:           "feature/add-oauth-login",
		SHA:           "1234567890abcdef",
		PRNumber:      42,
	}, Options{GeneratedAt: fixedTime()})

	if len(report.Checks) != 10 {
		t.Fatalf("expected 10 checks in report, got %d", len(report.Checks))
	}

	// Must have a status
	if report.Status == "" {
		t.Fatal("report status should not be empty")
	}
	// Score must be 0-100
	if report.Score < 0 || report.Score > 100 {
		t.Fatalf("score out of range: %d", report.Score)
	}
}

func TestBuildSelectedCheck(t *testing.T) {
	report := Build(Inputs{
		BranchName:    "BadBranch",
		CommitMessage: "feat(auth): add oauth login",
		PRBody:        validPRBody,
	}, Options{Check: "commit-message", GeneratedAt: fixedTime()})

	if len(report.Checks) != 1 {
		t.Fatalf("checks = %d, want 1", len(report.Checks))
	}
	if report.Checks[0].ID != "commit-message" {
		t.Fatalf("check ID = %s, want commit-message", report.Checks[0].ID)
	}
}

func TestWriteFiles(t *testing.T) {
	dir := t.TempDir()
	report := Build(Inputs{
		BranchName:    "feature/add-oauth-login",
		CommitMessage: validCommitMessage,
		PRBody:        validPRBody,
	}, Options{GeneratedAt: fixedTime()})

	if err := WriteFiles(report, dir); err != nil {
		t.Fatalf("WriteFiles() error = %v", err)
	}

	for _, name := range []string{"index.html", "ods-compliance.json", "ods-summary.md", "ods-compliance.svg", "ods-compliance.sarif"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "ods-compliance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed Report
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("report JSON should parse: %v", err)
	}
	if parsed.Status == "" {
		t.Fatal("parsed status should not be empty")
	}
	if len(parsed.Checks) != 10 {
		t.Fatalf("parsed checks = %d, want 10", len(parsed.Checks))
	}
}

func TestHTMLRendererEscapesCheckNotes(t *testing.T) {
	report := Build(Inputs{
		BranchName:    "feature/<script>alert(1)",
		CommitMessage: "feat(auth): add <script> login",
	}, Options{GeneratedAt: fixedTime()})

	page, err := HTML(report)
	if err != nil {
		t.Fatalf("HTML() error = %v", err)
	}
	// Check that raw HTML tags are escaped
	if strings.Contains(page, `feature/<script>`) && !strings.Contains(page, `&lt;script&gt;`) {
		t.Fatalf("HTML output contains unescaped script tag: %s", page)
	}
}

func TestFilterHumanReviewers(t *testing.T) {
	reviewers := []string{"jane-doe", "github-actions[bot]", "dependabot[bot]", "john-smith", "renovate[bot]"}
	humans := filterHumanReviewers(reviewers)
	if len(humans) != 2 {
		t.Fatalf("expected 2 human reviewers, got %d: %v", len(humans), humans)
	}
	if !contains(humans, "jane-doe") || !contains(humans, "john-smith") {
		t.Fatalf("expected jane-doe and john-smith, got %v", humans)
	}
}

func TestIsTestFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"auth_test.go", true},
		{"test_auth.go", true},
		{"src/__tests__/auth.js", true},
		{"auth.spec.ts", true},
		{"auth.go", false},
		{"README.md", false},
	}
	for _, tc := range cases {
		if got := isTestFile(tc.path); got != tc.want {
			t.Fatalf("isTestFile(%s) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestCheckWeight(t *testing.T) {
	if w := CheckWeight("ai-disclosure"); w != 10 {
		t.Fatalf("ai-disclosure weight = %d, want 10", w)
	}
	if w := CheckWeight("commit-message"); w != 2 {
		t.Fatalf("commit-message weight = %d, want 2", w)
	}
	if w := CheckWeight("unknown"); w != 5 {
		t.Fatalf("unknown weight = %d, want 5", w)
	}
}

func TestRequiredCICheckWithPRTrigger(t *testing.T) {
	c := checkRequiredCI(CheckInputs{
		CIWorkflowsExist:  true,
		CIWorkflowContent: "on:\n  pull_request:\n    types: [opened]\njobs:\n  build:\n    runs-on: ubuntu-latest",
	})
	if c.Status != CheckPass {
		t.Fatalf("expected pass with PR trigger, got %s", c.Status)
	}
}

func TestRequiredCICheckNoPRTrigger(t *testing.T) {
	c := checkRequiredCI(CheckInputs{
		CIWorkflowsExist:  true,
		CIWorkflowContent: "on: [push]\njobs:\n  build:\n    runs-on: ubuntu-latest",
	})
	if c.Status != CheckWarning {
		t.Fatalf("expected warning without PR trigger, got %s: %v", c.Status, c.Notes)
	}
}

func TestTestEvidenceCIOnly(t *testing.T) {
	// No changed files but CI has test step
	c := checkTestEvidence(CheckInputs{
		ChangedFiles:      []string{},
		CIWorkflowContent: "steps:\n  - run: go test ./...",
	})
	// Still skipped because changed files empty
	if c.Status != CheckSkipped {
		t.Fatalf("expected skipped with empty changed files, got %s", c.Status)
	}
}

func TestSecurityScanDetectsMultipleTools(t *testing.T) {
	c := checkSecurityScanEvidence(CheckInputs{
		CIWorkflowContent: "steps:\n  - uses: github/codeql-action/analyze@v3\n  - uses: snyk/actions/golang@master\n  - run: trivy fs .",
	})
	if c.Status != CheckPass {
		t.Fatalf("expected pass, got %s", c.Status)
	}
	found := 0
	for _, n := range c.Notes {
		if strings.Contains(n, "codeql") {
			found++
		}
	}
	if found == 0 {
		t.Fatalf("expected codeql in notes, got %v", c.Notes)
	}
}

func TestSARIFRulesCoverAllChecks(t *testing.T) {
	rules := buildSARIFRules()
	if len(rules) != 10 {
		t.Fatalf("expected 10 SARIF rules, got %d", len(rules))
	}
	ids := make(map[string]bool)
	for _, rule := range rules {
		ids[rule.ID] = true
	}
	for _, id := range []string{"ODS01", "ODS02", "ODS03", "ODS04", "ODS05", "ODS06", "ODS07", "ODS08", "ODS09", "ODS10"} {
		if !ids[id] {
			t.Fatalf("missing SARIF rule: %s", id)
		}
	}
}

func fixedTime() time.Time {
	return time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
}
