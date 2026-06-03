package report

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/open-delivery-spec/cli/internal/policy"
	"github.com/open-delivery-spec/cli/internal/validator"
)

// checkWeight maps check IDs to their scoring weight per the ODS scoring model.
var checkWeight = map[string]int{
	"ai-disclosure":            10, // Critical
	"human-review-evidence":    10, // Critical
	"required-ci":              7,  // High (7.5 rounded)
	"approval-policy":          7,  // High
	"ai-agent-commit-detection": 7, // High
	"test-evidence":            7,  // High
	"security-scan-evidence":   7,  // High
	"pr-description":           5,  // Medium
	"release-readiness":        5,  // Medium
	"commit-message":           2,  // Low (2.5 rounded)
}

// CheckInputs holds all data needed to run checks.
type CheckInputs struct {
	BranchName    string
	CommitMessage string
	PRBody        string
	// ReviewerLogins is a list of human reviewers (not bots) who approved the PR.
	ReviewerLogins []string
	// PRAuthor is the GitHub login of the PR author.
	PRAuthor string
	// CommitAuthorEmail is the email from the commit.
	CommitAuthorEmail string
	// CIWorkflowsExist is true if CI workflow files exist under .github/workflows/.
	CIWorkflowsExist bool
	// CIWorkflowContent holds the raw text of CI workflow files for scanning.
	CIWorkflowContent string
	// ChangedFiles is the list of files changed in the PR/commit.
	ChangedFiles []string
	// BranchProtectionEnabled indicates whether branch protection is on.
	BranchProtectionEnabled bool
	// RequiredApprovals is the number of required approvals from branch protection.
	RequiredApprovals int
	// CODEOWNERSExists is true if CODEOWNERS file exists.
	CODEOWNERSExists bool
	// ReleaseHasODSCheck is true if the release process references ODS.
	ReleaseHasODSCheck bool
}

// AllChecks builds all 10 checks from the ODS check directory.
func AllChecks(in CheckInputs, opts Options, p *policy.Policy) []Check {
	if p == nil {
		defaultP, _ := policy.LoadPolicy()
		p = defaultP
	}
	checks := []Check{
		checkAIDisclosure(in, p),
		checkHumanReviewEvidence(in),
		checkRequiredCI(in),
		checkApprovalPolicy(in),
		checkAIAgentCommitDetection(in),
		checkTestEvidence(in),
		checkSecurityScanEvidence(in),
		checkPRDescription(in, opts, p),
		checkReleaseReadiness(in),
		checkCommitMessage(in, opts, p),
	}
	return checks
}

// checkAIDisclosure checks for AI disclosure in commit messages, PR bodies, and config.
// This is the foundation check — without it, all other AI-related checks lose context.
func checkAIDisclosure(in CheckInputs, p *policy.Policy) Check {
	id := "ai-disclosure"
	name := "AI Disclosure"

	// If no commit message or PR body, we can't detect anything
	if strings.TrimSpace(in.CommitMessage) == "" && strings.TrimSpace(in.PRBody) == "" {
		return skipped(id, name, "no commit message or PR body available for AI disclosure detection")
	}

	check := Check{ID: id, Name: name, Status: CheckPass, Score: 10, Weight: 10}

	// 1. Check commit message for RAI trailers
	hasCommitDisclosure := false
	hasCommitAITool := false
	hasCoAuthoredByAI := false

	lines := strings.Split(in.CommitMessage, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "AI-assisted: true" {
			hasCommitDisclosure = true
		}
		if strings.HasPrefix(trimmed, "AI-tool:") && strings.TrimSpace(strings.TrimPrefix(trimmed, "AI-tool:")) != "" {
			hasCommitAITool = true
		}
		if strings.HasPrefix(trimmed, "Co-authored-by:") {
			if isAIRelatedTrailer(trimmed) {
				hasCoAuthoredByAI = true
			}
		}
		if strings.HasPrefix(trimmed, "Assisted-By:") {
			hasCoAuthoredByAI = true
		}
		if trimmed == "AI-generated: true" {
			hasCommitDisclosure = true
		}
	}

	// 2. Check PR body for AI Disclosure
	hasPRDisclosure := false
	hasPRAITool := false
	if strings.Contains(in.PRBody, "## AI Disclosure") || strings.Contains(in.PRBody, "## AI-Disclosure") {
		hasPRDisclosure = true
	}
	if strings.Contains(in.PRBody, "This PR contains AI-generated code") {
		hasPRDisclosure = true
	}
	if strings.Contains(in.PRBody, "AI Tool:") || strings.Contains(in.PRBody, "**AI Tool:") {
		hasPRAITool = true
	}

	anyDisclosure := hasCommitDisclosure || hasPRDisclosure || hasCoAuthoredByAI

	// 3. Policy-driven checks
	if p != nil && p.AIDisclosure.Required {
		if !anyDisclosure {
			check.Status = CheckFail
			check.Score = 0
			check.Errors = append(check.Errors,
				"AI disclosure is required by policy but no AI disclosure found in commit message or PR description")
			check.FixSuggestions = append(check.FixSuggestions, validator.FixSuggestion{
				Title:       "Add AI Disclosure",
				Description: "Your policy requires AI disclosure. Add AI-assisted trailers to commits or an AI Disclosure section to the PR.",
				Template: `Commit footer:
AI-assisted: true
AI-tool: <tool-name>

PR section:
## AI Disclosure
- [x] This PR contains AI-generated code
- **AI Tool:** <tool-name>
- **AI Scope:** <what was generated>
- **Human Review:** <what was reviewed>`,
			})
		}
	}

	// 4. If AI disclosure exists but AI-tool is missing → Fail
	if (hasCommitDisclosure && !hasCommitAITool) || (hasPRDisclosure && !hasPRAITool && hasCommitDisclosure) {
		aiSeverity := policy.SeverityError
		if p != nil {
			aiSeverity = p.GetSeverity("pr_ai_tool")
		}
		errMsg := "AI disclosure present but AI-tool is not specified — 'AI-assisted: true' requires 'AI-tool:' field"
		if aiSeverity == policy.SeverityWarning {
			check.Warnings = append(check.Warnings, errMsg)
			if check.Status == CheckPass {
				check.Status = CheckWarning
				check.Score = 5
			}
		} else {
			check.Status = CheckFail
			check.Score = 0
			check.Errors = append(check.Errors, errMsg)
			check.FixSuggestions = append(check.FixSuggestions, validator.FixSuggestion{
				Title:       "Specify AI Tool",
				Description: "When AI disclosure is present, you must specify which AI tool was used.",
				Template:    "AI-tool: GitHub Copilot",
			})
		}
	}

	// 5. If disclosure found with tool, it's compliant
	if anyDisclosure && len(check.Errors) == 0 {
		if check.Status == CheckPass {
			check.Status = CheckPass
			check.Score = 10
			details := []string{"AI disclosure detected"}
			if hasCommitDisclosure {
				details = append(details, "commit message contains AI-assisted trailers")
			}
			if hasPRDisclosure {
				details = append(details, "PR contains AI Disclosure section")
			}
			if hasCoAuthoredByAI {
				details = append(details, "AI co-authorship detected")
			}
			check.Notes = details
		}
	}

	// 6. If strict_tool_name policy is set but tool name is too vague
	if p != nil && p.AIDisclosure.StrictToolName && hasCommitAITool {
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "AI-tool:") {
				tool := strings.TrimSpace(strings.TrimPrefix(trimmed, "AI-tool:"))
				if isVagueToolName(tool) {
					check.Warnings = append(check.Warnings,
						fmt.Sprintf("AI-tool name '%s' is vague — consider using a specific tool name (e.g., 'GitHub Copilot', 'Claude', 'Cursor')", tool))
					if check.Status == CheckPass {
						check.Status = CheckWarning
						check.Score = 5
					}
				}
			}
		}
	}

	check.Value = disclosureSummary(anyDisclosure, hasCommitAITool, hasPRAITool)
	return check
}

// isAIRelatedTrailer checks if a trailer value looks like an AI tool name.
func isAIRelatedTrailer(trailer string) bool {
	aiNamePatterns := []string{
		"copilot", "claude", "chatgpt", "cursor", "codex",
		"gemini", "tabnine", "codeium", "anthropic", "openai",
		"ai", "assistant", "agent", "bot-ai", "ai-agent",
	}
	lower := strings.ToLower(trailer)
	for _, pattern := range aiNamePatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// isVagueToolName returns true if the tool name is not specific enough.
func isVagueToolName(name string) bool {
	vague := []string{"ai", "AI", "llm", "assistant", "agent", "bot", "tool"}
	trimmed := strings.TrimSpace(name)
	for _, v := range vague {
		if strings.EqualFold(trimmed, v) {
			return true
		}
	}
	return false
}

func disclosureSummary(hasDisclosure, hasTool, hasPRTool bool) string {
	if !hasDisclosure {
		return "no AI disclosure detected"
	}
	parts := []string{"AI disclosure found"}
	if hasTool || hasPRTool {
		parts = append(parts, "with tool specified")
	}
	return strings.Join(parts, ", ")
}

// checkCommitMessage runs the commit message check (Low weight: 2.5).
func checkCommitMessage(in CheckInputs, opts Options, p *policy.Policy) Check {
	id := "commit-message"
	name := "Commit Message"
	if strings.TrimSpace(in.CommitMessage) == "" {
		return skipped(id, name, "commit message not detected")
	}
	result, err := validator.ValidateCommitMessageWithPolicy(strings.TrimSpace(in.CommitMessage), p)
	c := checkFromResult(id, name, firstLine(in.CommitMessage), result, err, opts.Strict)
	// Override scoring with correct weight
	c = applyWeight(c, 2)
	return c
}

// checkPRDescription runs the PR description check (Medium weight: 5).
func checkPRDescription(in CheckInputs, opts Options, p *policy.Policy) Check {
	id := "pr-description"
	name := "PR Description"
	if strings.TrimSpace(in.PRBody) == "" {
		return skipped(id, name, "PR body not detected")
	}
	result, err := validator.ValidatePRDescriptionWithPolicy(in.PRBody, p)
	c := checkFromResult(id, name, "", result, err, opts.Strict)
	c = applyWeight(c, 5)
	return c
}

// checkHumanReviewEvidence validates that AI code has been reviewed by a human (Critical, weight 10).
// When an AI agent PR is detected, the check severity is elevated.
func checkHumanReviewEvidence(in CheckInputs) Check {
	id := "human-review-evidence"
	name := "Human Review Evidence"

	if len(in.ReviewerLogins) == 0 && in.PRAuthor == "" {
		return skipped(id, name, "review data not available — provide via GitHub event payload or ODS_REVIEWERS env var")
	}

	check := Check{ID: id, Name: name, Status: CheckPass, Score: 10, Weight: 10}

	// Detect human reviewers (filter out bot patterns)
	humanReviewers := filterHumanReviewers(in.ReviewerLogins)
	isAIAgentPR := in.PRAuthor != "" && isKnownAIAgentUsername(in.PRAuthor) != ""

	if len(humanReviewers) == 0 {
		check.Status = CheckFail
		check.Score = 0
		errMsg := "no human review evidence found — PR was not reviewed by a human reviewer"
		if isAIAgentPR {
			errMsg = fmt.Sprintf("CRITICAL: AI agent PR ('%s') has no human review — this is the highest-risk scenario", in.PRAuthor)
		}
		check.Errors = append(check.Errors, errMsg)
		fixTitle := "Request human review"
		fixDesc := "Ensure at least one human reviewer (non-bot) approves the PR before merge."
		if isAIAgentPR {
			fixTitle = "AI agent PR requires mandatory human review"
			fixDesc = "AI agent PRs must have at least one human reviewer who actually reads the code before merge."
		}
		check.FixSuggestions = append(check.FixSuggestions, validator.FixSuggestion{
			Title:       fixTitle,
			Description: fixDesc,
			Template:    "Request review from a team member and ensure they leave a comment or approval.",
		})
	} else {
		msg := fmt.Sprintf("human review detected: %d human reviewer(s)", len(humanReviewers))
		if isAIAgentPR {
			msg += fmt.Sprintf(" (AI agent PR '%s' has human oversight)", in.PRAuthor)
		}
		check.Notes = append(check.Notes, msg)
	}

	// Self-approve detection
	if in.PRAuthor != "" && contains(humanReviewers, in.PRAuthor) && len(humanReviewers) == 1 {
		check.Status = CheckFail
		check.Score = 0
		selfApproveMsg := fmt.Sprintf("self-approve detected: author '%s' is the only approver", in.PRAuthor)
		if isAIAgentPR {
			selfApproveMsg = fmt.Sprintf("CRITICAL: AI agent '%s' self-approved its own PR — this circumvents all human oversight", in.PRAuthor)
		}
		check.Errors = append(check.Errors, selfApproveMsg)
		check.FixSuggestions = append(check.FixSuggestions, validator.FixSuggestion{
			Title:       "Request additional review",
			Description: "Self-approvals are not sufficient. Request review from a different team member.",
			Template:    "Request review from another team member.",
		})
	}

	return check
}

// checkRequiredCI checks that CI is configured and triggered on PRs (High, weight 7.5).
func checkRequiredCI(in CheckInputs) Check {
	id := "required-ci"
	name := "Required CI"

	if !in.CIWorkflowsExist {
		c := Check{ID: id, Name: name, Status: CheckFail, Score: 0, Weight: 7}
		c.Errors = append(c.Errors, "no CI workflow configuration found — add .github/workflows/*.yml")
		c.FixSuggestions = append(c.FixSuggestions, validator.FixSuggestion{
			Title:       "Add CI workflow",
			Description: "Create a GitHub Actions workflow that runs on pull_request events.",
			Template: `name: CI
on:
  pull_request:
    types: [opened, synchronize, reopened]
  push:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: make test`,
		})
		return c
	}

	// Check if CI triggers on PR
	hasPRTrigger := strings.Contains(in.CIWorkflowContent, "pull_request")
	c := Check{ID: id, Name: name, Status: CheckPass, Score: 7, Weight: 7}
	c.Notes = append(c.Notes, "CI workflow detected")
	if !hasPRTrigger {
		c.Warnings = append(c.Warnings,
			"CI workflow exists but may not trigger on pull_request events")
		c.Status = CheckWarning
		c.Score = 4
	} else {
		c.Notes = append(c.Notes, "CI triggers on pull_request")
	}
	return c
}

// checkApprovalPolicy checks branch protection and approval rules (High, weight 7.5).
func checkApprovalPolicy(in CheckInputs) Check {
	id := "approval-policy"
	name := "Approval Policy"

	if !in.BranchProtectionEnabled && !in.CODEOWNERSExists {
		c := Check{ID: id, Name: name, Status: CheckFail, Score: 0, Weight: 7}
		c.Errors = append(c.Errors,
			"no approval policy detected: branch protection not enabled and no CODEOWNERS file found")
		c.FixSuggestions = append(c.FixSuggestions, validator.FixSuggestion{
			Title:       "Enable branch protection",
			Description: "Enable branch protection rules requiring PR reviews before merge.",
			Template:    "In GitHub: Settings → Branches → Add branch protection rule for 'main' → Require pull request reviews before merging",
		})
		return c
	}

	c := Check{ID: id, Name: name, Status: CheckPass, Score: 7, Weight: 7}
	details := []string{}
	if in.BranchProtectionEnabled {
		details = append(details, "branch protection enabled")
		if in.RequiredApprovals > 0 {
			details = append(details, fmt.Sprintf("requires %d approval(s)", in.RequiredApprovals))
		}
	}
	if in.CODEOWNERSExists {
		details = append(details, "CODEOWNERS file exists")
	}
	c.Notes = details
	return c
}

// checkAIAgentCommitDetection checks for AI agent patterns in commit authorship (High, weight 7.5).
func checkAIAgentCommitDetection(in CheckInputs) Check {
	id := "ai-agent-commit-detection"
	name := "AI Agent Commit Detection"

	c := Check{ID: id, Name: name, Status: CheckPass, Score: 7, Weight: 7}

	if in.CommitAuthorEmail == "" && in.PRAuthor == "" {
		return skipped(id, name, "commit author email not available")
	}

	agentPattern := matchAIAgentPattern(in.CommitAuthorEmail)
	agentUsername := ""
	if in.PRAuthor != "" {
		agentUsername = isKnownAIAgentUsername(in.PRAuthor)
	}

	if agentPattern != "" || agentUsername != "" {
		c.Status = CheckWarning
		c.Score = 4
		if agentPattern != "" {
			c.Warnings = append(c.Warnings,
				fmt.Sprintf("commit author email matches AI agent pattern: %s", agentPattern))
			c.Notes = append(c.Notes, fmt.Sprintf("AI agent detected via email: %s", agentPattern))
		}
		if agentUsername != "" {
			c.Warnings = append(c.Warnings,
				fmt.Sprintf("PR author '%s' matches known AI agent username pattern: %s", in.PRAuthor, agentUsername))
			c.Notes = append(c.Notes, fmt.Sprintf("AI agent detected via username: %s", agentUsername))
		}
		c.FixSuggestions = append(c.FixSuggestions, validator.FixSuggestion{
			Title:       "AI agent commit detected — ensure human review",
			Description: "This commit appears to be authored by an AI agent. Ensure a human has reviewed the code and the PR includes an AI Disclosure section.",
			Template: `## AI Disclosure
- [x] This PR contains AI-generated code
- **AI Tool:** [name of AI tool]
- **AI Scope:** [what was generated]
- **Human Review:** [what was reviewed by a human]`,
		})
	} else {
		c.Notes = append(c.Notes, "no AI agent patterns detected in commit author or PR author")
	}

	return c
}

// known AI agent commit patterns
var aiAgentPatterns = []struct {
	pattern *regexp.Regexp
	label   string
}{
	{regexp.MustCompile(`(?i)\+ai-`), "AI agent email prefix (+ai-)"},
	{regexp.MustCompile(`(?i)@ai\.`), "AI email domain (@ai.*)"},
	{regexp.MustCompile(`(?i)noreply@github\.com`), "possible GitHub bot (noreply)"},
	{regexp.MustCompile(`(?i)@openai\.com`), "OpenAI email domain"},
	{regexp.MustCompile(`(?i)@anthropic\.com`), "Anthropic email domain"},
	{regexp.MustCompile(`(?i)copilot-`), "GitHub Copilot agent"},
	{regexp.MustCompile(`(?i)claude[.-]`), "Claude agent"},
	{regexp.MustCompile(`(?i)cursor[.-]`), "Cursor agent"},
	{regexp.MustCompile(`(?i)codeium[.-]`), "Codeium/Windsurf agent"},
	{regexp.MustCompile(`(?i)tabnine[.-]`), "Tabnine agent"},
	{regexp.MustCompile(`(?i)@amazon\.(com|ai)`), "Amazon Q Developer"},
	{regexp.MustCompile(`(?i)@microsoft\.[^.]*$`), "Microsoft-owned AI agent"},
	{regexp.MustCompile(`(?i)swe-agent`), "SWE-agent (autonomous)"},
	{regexp.MustCompile(`(?i)devika[.-]`), "Devika agent"},
	{regexp.MustCompile(`(?i)openhands[.-]`), "OpenHands agent"},
	{regexp.MustCompile(`(?i)aider[.-]`), "Aider AI agent"},
	{regexp.MustCompile(`(?i)cody[.-]`), "Sourcegraph Cody agent"},
	{regexp.MustCompile(`(?i)mentat[.-]`), "Mentat agent"},
	{regexp.MustCompile(`(?i)gpt-pilot`), "GPT Pilot agent"},
	{regexp.MustCompile(`(?i)bot[+\-_]ai`), "generic bot+ai pattern"},
}

func matchAIAgentPattern(email string) string {
	for _, ap := range aiAgentPatterns {
		if ap.pattern.MatchString(email) {
			return ap.label
		}
	}
	return ""
}

func isKnownAIAgentUsername(username string) string {
	agentUsernames := []string{
		"github-actions[bot]", "dependabot[bot]", "renovate[bot]",
		"codecov[bot]", "vercel[bot]", "netlify[bot]",
		"copilot-pull-request-reviewer",
	}
	for _, agent := range agentUsernames {
		if strings.EqualFold(username, agent) {
			return agent
		}
	}
	// Pattern-based detection for usernames
	agentPatterns := []string{"[bot]", "-bot-", "copilot", "claude-dev", "cursor-ai"}
	for _, p := range agentPatterns {
		if strings.Contains(strings.ToLower(username), p) {
			return p
		}
	}
	return ""
}

// checkTestEvidence checks for test files and test steps in CI (High, weight 7.5).
func checkTestEvidence(in CheckInputs) Check {
	id := "test-evidence"
	name := "Test Evidence"

	if len(in.ChangedFiles) == 0 {
		return skipped(id, name, "no changed files available for test evidence detection")
	}

	c := Check{ID: id, Name: name, Status: CheckPass, Score: 7, Weight: 7}

	hasTests := false
	for _, f := range in.ChangedFiles {
		if isTestFile(f) {
			hasTests = true
			break
		}
	}

	if !hasTests {
		c.Warnings = append(c.Warnings,
			"no test files detected in changes — consider adding tests for changed code")
		c.Status = CheckWarning
		c.Score = 4
		c.FixSuggestions = append(c.FixSuggestions, validator.FixSuggestion{
			Title:       "Add tests",
			Description: "Add test files to cover the changed code.",
			Template:    "Create test files alongside changed source files.",
		})
	} else {
		c.Notes = append(c.Notes, "test files detected in changes")
	}

	// Also check CI workflow for test steps (even without changed files)
	ciTestPatterns := []string{
		"go test", "npm test", "pytest", "cargo test", "mix test",
		"rspec", "jest", "mocha", "phpunit", "bundle exec",
		"make test", "gradle test", "mvn test", ".NET test", "dotnet test",
	}
	ciHasTests := false
	for _, p := range ciTestPatterns {
		if strings.Contains(strings.ToLower(in.CIWorkflowContent), strings.ToLower(p)) {
			ciHasTests = true
			break
		}
	}
	if ciHasTests {
		c.Notes = append(c.Notes, "CI workflow includes test step")
	}

	return c
}

// isTestFile checks if a file path looks like a test file.
func isTestFile(path string) bool {
	testPatterns := []string{
		"_test.go", ".test.go", "test_", "_test.py", ".test.py",
		".test.ts", ".spec.ts", ".test.js", ".spec.js", ".test.java",
		"Test.java", "Tests.java", "__tests__/", "/test/", "/tests/",
	}
	for _, p := range testPatterns {
		if strings.Contains(path, p) {
			return true
		}
	}
	return false
}

// checkSecurityScanEvidence checks for security scanning in CI (High, weight 7.5).
func checkSecurityScanEvidence(in CheckInputs) Check {
	id := "security-scan-evidence"
	name := "Security Scan Evidence"

	c := Check{ID: id, Name: name, Status: CheckPass, Score: 7, Weight: 7}

	if in.CIWorkflowContent == "" {
		return skipped(id, name, "no CI workflow content available for security scan detection")
	}

	securityTools := []string{
		"codeql", "snyk", "semgrep", "trivy", "gosec", "bandit",
		"dependabot", "npm audit", "owasp", "safety", "brakeman",
		"tfsec", "checkov", "dockle", "hadolint",
		"gitleaks", "trufflehog", "detect-secrets", "secretlint",
		"zap", "arachni", "nikto", "sqlmap",
		"kubescape", "kube-bench", "falco",
		"dependency-check", "osv-scanner", "grype",
		"sast", "dast", "sca", // generic scan types
	}

	detectedTools := []string{}
	for _, tool := range securityTools {
		if strings.Contains(strings.ToLower(in.CIWorkflowContent), tool) {
			detectedTools = append(detectedTools, tool)
		}
	}

	if len(detectedTools) == 0 {
		c.Status = CheckWarning
		c.Score = 4
		c.Warnings = append(c.Warnings,
			"no security scanning tool detected in CI workflow")
		c.FixSuggestions = append(c.FixSuggestions, validator.FixSuggestion{
			Title:       "Add security scanning",
			Description: "Integrate a security scanning tool (CodeQL, Snyk, Semgrep, Trivy) into your CI pipeline.",
			Template: `# In .github/workflows/ci.yml:
- name: Run CodeQL
  uses: github/codeql-action/analyze@v3
- name: Run Snyk
  uses: snyk/actions/golang@master`,
		})
	} else {
		c.Notes = append(c.Notes,
			fmt.Sprintf("security scanning detected: %s", strings.Join(detectedTools, ", ")))
	}

	return c
}

// checkReleaseReadiness checks if release process integrates ODS checks (Medium, weight 5).
func checkReleaseReadiness(in CheckInputs) Check {
	id := "release-readiness"
	name := "Release Readiness"

	c := Check{ID: id, Name: name, Status: CheckPass, Score: 5, Weight: 5}

	if in.ReleaseHasODSCheck {
		c.Notes = append(c.Notes, "release process integrates ODS checks")
	} else {
		c.Status = CheckWarning
		c.Score = 2
		c.Warnings = append(c.Warnings,
			"release process does not appear to integrate ODS compliance checks")
		c.FixSuggestions = append(c.FixSuggestions, validator.FixSuggestion{
			Title:       "Integrate ODS in releases",
			Description: "Add ODS compliance check as a release gate to ensure AI disclosure and review requirements are met before deployment.",
			Template:    `ods report --format json --output ods-report`,
		})
	}

	return c
}

// applyWeight recalculates the score for a check based on the check weight.
// The score represents pass (full weight), warning (half weight), or fail (0).
func applyWeight(c Check, weight int) Check {
	switch c.Status {
	case CheckPass:
		c.Score = weight
	case CheckWarning:
		c.Score = weight / 2
	case CheckFail:
		c.Score = 0
	case CheckSkipped:
		c.Score = 0
	}
	return c
}

// contains checks if a string slice contains a specific string (case-insensitive for usernames).
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, item) {
			return true
		}
	}
	return false
}

// filterHumanReviewers filters out bot reviewers from a list.
func filterHumanReviewers(reviewers []string) []string {
	botPatterns := []string{"[bot]", "-bot", "dependabot", "renovate", "codecov", "stale"}
	var humans []string
	for _, r := range reviewers {
		isBot := false
		for _, bp := range botPatterns {
			if strings.Contains(strings.ToLower(r), bp) {
				isBot = true
				break
			}
		}
		if !isBot {
			humans = append(humans, r)
		}
	}
	return humans
}

// Export helper for tests and CLI.
func CheckWeight(id string) int {
	w, ok := checkWeight[id]
	if !ok {
		return 5 // default medium
	}
	return w
}

// FormatJSON marshals the report to JSON bytes.
func FormatJSON(r Report) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// FormatSARIF generates SARIF bytes from the report.
func FormatSARIF(r Report) ([]byte, error) {
	return SARIF(r)
}

// SummarizeChecks computes score and status from a list of checks.
// Exported for CLI use when running selected checks.
func SummarizeChecks(checks []Check) (int, Status) {
	return summarize(checks)
}
