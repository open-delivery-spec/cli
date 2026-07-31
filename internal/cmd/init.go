package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const odsWorkflow = `name: ODS AI Code Quality
on:
  pull_request:
    types: [opened, synchronize, reopened]

jobs:
  ods:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0
      - uses: open-delivery-spec/validate-action@v1
        with:
          diff-base: ${{ github.event.pull_request.base.sha }}
          pr-body: ${{ github.event.pull_request.body }}
          branch: ${{ github.head_ref }}
          commits: ${{ github.event.pull_request.commits }}
`

const defaultPolicy = `package ods.policy

# ODS policy for this repository.
# Edit the rules below to enforce your team's AI code quality requirements.
# Documentation: https://github.com/open-delivery-spec/spec

default allow := true

# Block AI code with critical quality issues.
deny[msg] {
    issue := input.issues[_]
    issue.severity == "critical"
    msg = sprintf("CRITICAL: %s at %s:%d", [issue.rule, issue.file, issue.line])
}

# Warn when high-confidence AI code has multiple issues.
warn[msg] {
    input.ai_generated == true
    input.ai_confidence > 0.8
    count(input.issues) > 2
    msg = "High-confidence AI code with multiple quality issues — enhanced review recommended"
}

# --- Disclosure completeness ---
# SFC guidance and the kernel docs both require authors to disclose AI
# involvement (what tool, what model, how it participated). These rules make
# that norm checkable: AI signals without any author attribution get flagged
# and routed to extra review — never blocked.
ai_disclosed {
    input.detection_sources[_] == "commit-trailer"
}

ai_disclosed {
    input.detection_sources[_] == "git-ai-notes"
}

ai_disclosed {
    input.detection_sources[_] == "pr-body"
}

ai_undisclosed {
    input.ai_generated == true
    # Suspicion threshold for routing: tune to your tolerance for heuristic
    # false positives (branch prefix alone scores 0.35).
    input.ai_confidence >= 0.5
    not ai_disclosed
}

warn[msg] {
    input.ai_generated == true
    not ai_disclosed
    msg = "AI code detected without author disclosure — ask for attribution (Co-Authored-By/Assisted-by trailer, or an AI disclosure in the PR body)"
}

# --- Review routing (optional) ---
# review_tier tells CI how much human attention this change needs:
#   "auto"     — low risk: eligible for expedited review or auto-merge
#   "standard" — normal review (the default)
#   "elevated" — high risk: request extra reviewers
# deny always wins — a blocked PR is never routed. Tune the conditions to your
# team; e.g. add "input.test_coverage >= 0.6" to auto if you publish coverage.
default review_tier := "standard"

review_tier := "auto" {
    input.technical_debt_delta <= 1.0
    not has_high_or_critical
    not ai_review_requests_changes
    not ai_undisclosed
    not ai_undertested
    not ai_touches_risky_path
}

review_tier := "elevated" {
    input.ai_generated == true
    has_high_or_critical
}

# Suspected-but-undisclosed AI costs review attention, never a merge.
review_tier := "elevated" {
    ai_undisclosed
}

# AI reviewer verdicts (input.ai_reviews) are probabilistic: by default they
# only tighten the gate — a request_changes routes extra human review, never
# denies. Write your own deny over input.ai_reviews to opt in to enforcement.
review_tier := "elevated" {
    ai_review_requests_changes
}

ai_review_requests_changes {
    input.ai_reviews[_].verdict == "request_changes"
}

# --- Merge-confidence (deterministic diff facts) ---
# Facts derived from the diff alone: was source added without tests, does the
# change touch sensitive paths (CI config, dependency manifests, auth/crypto).
# They warn always, and route AI-authored changes to elevated. Deny stays
# opt-in — add your own deny over input.merge_confidence to enforce.
warn[msg] {
    input.merge_confidence.added_source_without_tests
    msg = "Source code changed but no tests were added or updated"
}

warn[msg] {
    p := input.merge_confidence.risky_paths[_]
    msg = sprintf("Change touches a sensitive path (%s) — extra review recommended", [p])
}

ai_undertested {
    input.ai_generated == true
    input.merge_confidence.added_source_without_tests
}

ai_touches_risky_path {
    input.ai_generated == true
    input.merge_confidence.risky_paths[_]
}

review_tier := "elevated" {
    ai_undertested
}

review_tier := "elevated" {
    ai_touches_risky_path
}

has_high_or_critical {
    input.issues[_].severity == "critical"
}

has_high_or_critical {
    input.issues[_].severity == "high"
}
`

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold ODS configuration for a project",
	Long: `Initialize ODS in your repository with a single command.

Scaffolds:
  • .github/workflows/ods-ai-quality.yml  — CI workflow for AI code quality checks
  • .ods/policy.rego                       — OPA Rego policy (edit to add custom rules)

Examples:
  ods init`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	workflowsDir := filepath.Join(".github", "workflows")
	odsDir := ".ods"

	for _, d := range []string{".github", workflowsDir, odsDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	files := map[string]string{
		filepath.Join(workflowsDir, "ods-ai-quality.yml"): odsWorkflow,
		filepath.Join(odsDir, "policy.rego"):              defaultPolicy,
	}

	for path, content := range files {
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("  ⏭️  Skipped (already exists): %s\n", path)
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		fmt.Printf("  ✅ Created: %s\n", path)
	}

	fmt.Println()
	fmt.Println("── ODS initialized ──")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Edit .ods/policy.rego to add custom enforcement rules")
	fmt.Println("  2. Install git hooks:  ods hook install")
	fmt.Println("  3. Commit and push — ODS will run on your next PR")

	return nil
}
