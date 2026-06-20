package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/open-delivery-spec/cli/internal/analyzer"
	"github.com/open-delivery-spec/cli/internal/detector"
	"github.com/open-delivery-spec/cli/internal/policy"
	"github.com/open-delivery-spec/cli/internal/scorer"
	"github.com/spf13/cobra"
)

var (
	checkPolicyFile string
	checkJSON       bool
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Evaluate OPA Rego policy against a change",
	Long: `Evaluate an OPA Rego policy against the current change.

ODS uses OPA (Open Policy Agent) as its enterprise policy engine.
Write policies in Rego to define custom blocking rules.

Place a policy at .ods/policy.rego:

  package ods.policy

  default allow := true

  deny[msg] {
      input.ai_confidence > 0.8
      input.test_coverage < 0.3
      msg = "AI code with low test coverage"
  }

Examples:
  ods check                                    # use .ods/policy.rego or default
  ods check --policy .ods/policy.rego          # explicit policy file
  ods check --json                             # JSON output`,
	RunE: runCheck,
}

func init() {
	rootCmd.AddCommand(checkCmd)
	checkCmd.Flags().StringVarP(&checkPolicyFile, "policy", "p", "", "path to Rego policy file")
	checkCmd.Flags().BoolVar(&checkJSON, "json", false, "output as JSON")
}

func runCheck(cmd *cobra.Command, args []string) error {
	// Resolve diff base: ODS_DIFF_BASE is set by validate-action to the PR base SHA.
	// Prefer it over the hardcoded HEAD~1 so the full PR diff is analysed rather than
	// only the most-recent commit.
	diffBase := "HEAD~1"
	if envBase := os.Getenv("ODS_DIFF_BASE"); envBase != "" {
		diffBase = envBase
	}

	// Discover policy file
	policyPath := checkPolicyFile
	if policyPath == "" {
		policyPath = policy.DiscoverRegoFile(".")
	}
	useDefault := false
	if policyPath == "" {
		useDefault = true
	}

	// Run detector
	detectOpts := detector.Options{DiffBase: diffBase, MaxCommits: 10}
	branch := detectBranch
	if branch == "" {
		// try env (check both names for compatibility)
		branch = os.Getenv("ODS_BRANCH")
	}
	if branch == "" {
		branch = os.Getenv("ODS_BRANCH_NAME")
	}
	if branch != "" {
		detectOpts.BranchName = branch
	}
	detectResult, err := detector.Detect(detectOpts)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: detection failed: %v\n", err)
		detectResult = &detector.DetectionResult{}
	}

	// Run analyzer on the changed files for this diff range
	diffFiles, _ := getGitDiffFiles(diffBase)
	var analyzeResult *analyzer.AnalysisResult
	if len(diffFiles) > 0 {
		analyzeResult = analyzer.Analyze(analyzer.Options{Files: diffFiles})
	} else {
		analyzeResult = &analyzer.AnalysisResult{}
	}

	// Derive TotalChangedLines and TestLines from the actual diff rather than from
	// AI-detected file counts (which fall back to 1 when nothing is detected and
	// cause TestCoverage and AICodeRatio to use a degenerate denominator).
	totalLines := 0
	testLines := 0
	for path, lines := range diffFiles {
		totalLines += len(lines)
		if strings.HasSuffix(path, "_test.go") {
			testLines += len(lines)
		}
	}
	if totalLines == 0 {
		for _, f := range detectResult.Files {
			totalLines += f.TotalLines
		}
	}
	if totalLines == 0 {
		totalLines = 1
	}

	scoreResult := scorer.Score(scorer.Options{
		DetectorResult:    detectResult,
		AnalyzerResult:    analyzeResult,
		TestLines:         testLines,
		TotalChangedLines: totalLines,
	})

	// Build list of changed file paths
	var changedFiles []string
	for f := range diffFiles {
		changedFiles = append(changedFiles, f)
	}

	// Build policy input
	evalInput := &policy.EvalInput{
		AIGenerated:        detectResult.AIGenerated,
		AIConfidence:       detectResult.Confidence,
		TechnicalDebtDelta: scoreResult.TechnicalDebtDelta,
		TestCoverage:       scoreResult.Breakdown.TestCoverage,
		Branch:             detectOpts.BranchName,
		ChangedFiles:       changedFiles,
	}

	for _, f := range detectResult.Files {
		evalInput.AIFiles = append(evalInput.AIFiles, policy.EvalFileInfo{
			Path:       f.Path,
			AILines:    f.AILines,
			TotalLines: f.TotalLines,
			Confidence: f.Confidence,
		})
	}

	for _, issue := range analyzeResult.Issues {
		evalInput.Issues = append(evalInput.Issues, policy.EvalIssue{
			Rule:     issue.Rule,
			File:     issue.File,
			Line:     issue.Line,
			Severity: issue.Severity,
			Message:  issue.Message,
		})
	}

	// Evaluate policy
	var result *policy.EvalResult
	if useDefault {
		// Use embedded default policy
		result, err = evaluateDefaultPolicy(evalInput)
	} else {
		result, err = policy.Evaluate(policyPath, evalInput)
	}
	if err != nil {
		return fmt.Errorf("policy evaluation failed: %w", err)
	}

	switch {
	case checkJSON:
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		cmd.OutOrStdout().Write(data)
		cmd.OutOrStdout().Write([]byte("\n"))
	default:
		printCheckResult(cmd, result, policyPath, useDefault)
	}

	if !result.Allowed {
		cmd.SilenceUsage = true
		return fmt.Errorf("policy denied: %d denial(s)", len(result.Denials))
	}
	return nil
}

func evaluateDefaultPolicy(input *policy.EvalInput) (*policy.EvalResult, error) {
	// Write default policy to temp file
	tmpFile, err := os.CreateTemp("", "ods-policy-*.rego")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(policy.DefaultRegoPolicy()); err != nil {
		return nil, err
	}
	tmpFile.Close()

	return policy.Evaluate(tmpFile.Name(), input)
}

func printCheckResult(cmd *cobra.Command, result *policy.EvalResult, policyPath string, useDefault bool) {
	if result.Allowed {
		fmt.Fprintf(cmd.OutOrStdout(), "✅  Policy check passed\n")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "❌  Policy check failed\n")
	}

	if useDefault {
		fmt.Fprintf(cmd.OutOrStdout(), "   Policy: default (built-in)\n")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "   Policy: %s\n", policyPath)
	}

	if len(result.Denials) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "   Denials:\n")
		for _, d := range result.Denials {
			fmt.Fprintf(cmd.OutOrStdout(), "     ❌ %s\n", d)
		}
	}

	if len(result.Warnings) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "   Warnings:\n")
		for _, w := range result.Warnings {
			fmt.Fprintf(cmd.OutOrStdout(), "     ⚠️  %s\n", w)
		}
	}
}
