package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/open-delivery-spec/cli/internal/report"
	"github.com/spf13/cobra"
)

// checkDoc holds documentation for each check.
type checkDoc struct {
	ID          string
	Name        string
	Weight      int
	Severity    string
	Description string
	WhyItMatters string
	HowToFix    string
}

var checksDocs = map[string]checkDoc{
	"ai-disclosure": {
		ID:            "ai-disclosure",
		Name:          "AI Disclosure",
		Weight:        10,
		Severity:      "Critical",
		Description:   "Detects whether AI-generated code is properly disclosed in commits (RAI trailers) and PR descriptions (AI Disclosure section).",
		WhyItMatters:  "Without AI disclosure, you cannot track which code was AI-generated — making it impossible to audit AI's safety impact, review quality, or attribution. This is the foundation check for all other AI safety measures.",
		HowToFix:      "Add AI-assisted trailers to commits (AI-assisted: true, AI-tool: <name>) and an AI Disclosure section to PR descriptions.",
	},
	"human-review-evidence": {
		ID:            "human-review-evidence",
		Name:          "Human Review Evidence",
		Weight:        10,
		Severity:      "Critical",
		Description:   "Verifies that PRs have been reviewed by at least one human (non-bot) reviewer. Detects self-approvals and AI agent PRs with no human oversight.",
		WhyItMatters:  "80% of PRs with AI review tools have zero human comments. PR approval ≠ human review. This is the highest-ROI check in the AI era.",
		HowToFix:      "Ensure a human team member reviews and approves the PR. Self-approvals are not sufficient.",
	},
	"required-ci": {
		ID:            "required-ci",
		Name:          "Required CI",
		Weight:        7,
		Severity:      "High",
		Description:   "Checks that a CI pipeline is configured (.github/workflows/) and triggers on pull_request events.",
		WhyItMatters:  "AI-generated code needs the same basic safety net as human code. No CI = no automated testing, no linting, no build verification.",
		HowToFix:      "Create a .github/workflows/ci.yml that runs on pull_request events with at minimum build and test steps.",
	},
	"approval-policy": {
		ID:            "approval-policy",
		Name:          "Approval Policy",
		Weight:        7,
		Severity:      "High",
		Description:   "Checks that branch protection rules are configured (required approvals, CODEOWNERS file).",
		WhyItMatters:  "Policy-layer check. Even with human review evidence, you need rules that enforce reviews before merge. Policy + Evidence = defense in depth.",
		HowToFix:      "Enable branch protection in GitHub Settings → Branches. Add a CODEOWNERS file for critical paths.",
	},
	"ai-agent-commit-detection": {
		ID:            "ai-agent-commit-detection",
		Name:          "AI Agent Commit Detection",
		Weight:        7,
		Severity:      "High",
		Description:   "Detects commits authored by AI agents (GitHub Copilot, Claude, Cursor, etc.) by analyzing commit author patterns.",
		WhyItMatters:  "AI agent commits without human review are the highest-risk scenario. Detection enables targeted review requirements.",
		HowToFix:      "If AI agent commits are detected, ensure each has a corresponding human review. Add AI disclosure to the commit or PR.",
	},
	"test-evidence": {
		ID:            "test-evidence",
		Name:          "Test Evidence",
		Weight:        7,
		Severity:      "High",
		Description:   "Detects whether test files are present in changed code and whether CI includes test steps.",
		WhyItMatters:  "AI-generated code most commonly lacks tests, especially security edge cases and boundary conditions.",
		HowToFix:      "Add test files alongside changed source files. Ensure CI runs tests automatically.",
	},
	"security-scan-evidence": {
		ID:            "security-scan-evidence",
		Name:          "Security Scan Evidence",
		Weight:        7,
		Severity:      "High",
		Description:   "Detects whether a security scanning tool (CodeQL, Snyk, Semgrep, Trivy, etc.) is integrated in CI.",
		WhyItMatters:  "25% of AI-generated code contains confirmed security vulnerabilities. A security scan in CI is the minimum defense.",
		HowToFix:      "Add a security scanning tool to your CI pipeline. GitHub CodeQL is free for public repos.",
	},
	"commit-message": {
		ID:            "commit-message",
		Name:          "Commit Message",
		Weight:        2,
		Severity:      "Low",
		Description:   "Validates commit messages follow Conventional Commits format with optional AI attribution trailers.",
		WhyItMatters:  "Structured commit metadata enables automated tracking of AI code percentage, fault attribution, and review chains.",
		HowToFix:      "Use format: <type>[scope]: <description>. Add AI-assisted: true and AI-tool: <name> trailers when AI was used.",
	},
	"pr-description": {
		ID:            "pr-description",
		Name:          "PR Description",
		Weight:        5,
		Severity:      "Medium",
		Description:   "Validates PR descriptions include required sections (Summary, Type, AI Disclosure, Changes, Testing, Checklist).",
		WhyItMatters:  "A well-structured PR description with AI disclosure creates an audit trail and sets clear expectations for reviewers.",
		HowToFix:      "Use the PR template. Include all required sections, especially AI Disclosure when AI-generated code is present.",
	},
	"release-readiness": {
		ID:            "release-readiness",
		Name:          "Release Readiness",
		Weight:        5,
		Severity:      "Medium",
		Description:   "Checks that the release process integrates ODS compliance checks (AI disclosure, review evidence, CI gates).",
		WhyItMatters:  "ODS checks should be release gates, not just PR checks. Without release integration, non-compliant code can still ship.",
		HowToFix:      "Add `ods report` to your release pipeline. Include ODS results as a release gate.",
	},
}

var checksCmd = &cobra.Command{
	Use:   "checks",
	Short: "List and explain ODS compliance checks",
	Long:  `List all ODS compliance checks or get detailed explanations for specific checks.`,
}

var checksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available checks",
	Long:  `List all ODS compliance checks with their IDs, names, weights, and severity levels.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("ODS Compliance Checks")
		fmt.Println(strings.Repeat("=", 76))
		fmt.Printf("%-30s %-25s %-8s %-10s\n", "ID", "Name", "Weight", "Severity")
		fmt.Println(strings.Repeat("-", 76))

		// Sort by weight descending (Critical first)
		ids := make([]string, 0, len(checksDocs))
		for id := range checksDocs {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			return checksDocs[ids[i]].Weight > checksDocs[ids[j]].Weight
		})

		for _, id := range ids {
			doc := checksDocs[id]
			fmt.Printf("%-30s %-25s %-8d %-10s\n", doc.ID, doc.Name, doc.Weight, doc.Severity)
		}

		fmt.Println()
		fmt.Printf("Total: %d checks\n", len(checksDocs))
		fmt.Println()
		fmt.Println("Use 'ods checks explain <check-id>' for detailed information about a specific check.")
		return nil
	},
}

var checksExplainCmd = &cobra.Command{
	Use:   "explain <check-id>",
	Short: "Explain a specific check in detail",
	Long: `Get a detailed explanation of a specific ODS compliance check, including:
- What it measures
- Why it matters
- How to fix failures
- How policy affects it`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		doc, ok := checksDocs[id]
		if !ok {
			// Try fuzzy match suggestions
			var suggestions []string
			for k := range checksDocs {
				if strings.Contains(k, id) || strings.Contains(id, k) {
					suggestions = append(suggestions, k)
				}
			}
			msg := fmt.Sprintf("unknown check: '%s'", id)
			if len(suggestions) > 0 {
				msg += fmt.Sprintf("\nDid you mean: %s?", strings.Join(suggestions, ", "))
			}
			msg += "\nUse 'ods checks list' to see all available checks."
			return fmt.Errorf("%s", msg)
		}

		fmt.Printf("Check: %s (%s)\n", doc.Name, doc.ID)
		fmt.Printf("Weight: %d — Severity: %s\n", doc.Weight, doc.Severity)
		fmt.Println()
		fmt.Println("── What it measures ──")
		fmt.Println(doc.Description)
		fmt.Println()
		fmt.Println("── Why it matters ──")
		fmt.Println(doc.WhyItMatters)
		fmt.Println()
		fmt.Println("── How to fix ──")
		fmt.Println(doc.HowToFix)
		fmt.Println()

		// Also show the weight relative to other checks
		w := report.CheckWeight(id)
		fmt.Printf("Scoring: This check contributes up to %d points to your total score.\n", w)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(checksCmd)
	checksCmd.AddCommand(checksListCmd)
	checksCmd.AddCommand(checksExplainCmd)
}
