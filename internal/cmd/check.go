package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/open-delivery-spec/cli/internal/analyzer"
	"github.com/open-delivery-spec/cli/internal/coverage"
	"github.com/open-delivery-spec/cli/internal/detector"
	"github.com/open-delivery-spec/cli/internal/logx"
	"github.com/open-delivery-spec/cli/internal/mergeconf"
	"github.com/open-delivery-spec/cli/internal/policy"
	"github.com/open-delivery-spec/cli/internal/review"
	"github.com/open-delivery-spec/cli/internal/sarif"
	"github.com/open-delivery-spec/cli/internal/scorer"
	"github.com/spf13/cobra"
)

var (
	checkPolicyFile string
	checkJSON       bool
	checkSARIF      string
	checkDiffBase   string
	checkAIReviews  []string
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
	checkCmd.Flags().StringVar(&checkSARIF, "sarif", "",
		"SARIF v2.1.0 file whose findings are merged into the policy input")
	checkCmd.Flags().StringVar(&checkDiffBase, "diff-base", "",
		"git ref to diff against (default: $ODS_DIFF_BASE or HEAD~1)")
	checkCmd.Flags().StringArrayVar(&checkAIReviews, "ai-review", nil,
		"AI review verdict file (ods.dev/review-verdict/v1); repeatable. Advisory by default: routes review attention, never denies unless your policy opts in")
}

func runCheck(cmd *cobra.Command, args []string) error {
	// Resolve diff base: ODS_DIFF_BASE is set by validate-action to the PR base SHA.
	// Prefer it over the hardcoded HEAD~1 so the full PR diff is analysed rather than
	// only the most-recent commit.
	diffBase := resolveDiffBase(checkDiffBase)
	// Keep the duplication estimator (which reads ODS_DIFF_BASE) on the same range.
	os.Setenv("ODS_DIFF_BASE", diffBase)
	logx.Debugf("check: diff base = %s", diffBase)

	// Discover policy file
	policyPath := checkPolicyFile
	if policyPath == "" {
		policyPath = policy.DiscoverRegoFile(".")
	}
	useDefault := false
	if policyPath == "" {
		useDefault = true
	}
	if useDefault {
		logx.Debugf("check: no policy file found, using built-in default policy")
	} else {
		logx.Debugf("check: using policy file %s", policyPath)
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

	// Merge external SARIF findings so the policy gate (and the score below) act
	// on authoritative analyzer results, not just the built-in heuristics.
	if checkSARIF != "" {
		if iss, err := sarif.Load(checkSARIF); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: loading SARIF file: %v\n", err)
		} else {
			analyzeResult.Issues = append(analyzeResult.Issues, iss...)
			analyzeResult.Summary = analyzer.ResummarizeSARIF(analyzeResult.Issues)
			logx.Debugf("check: merged %d SARIF finding(s) from %s", len(iss), checkSARIF)
		}
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

	// Auto-detect coverage report; pass -1 sentinel when not found so the
	// coverage penalty is not applied to PRs with no coverage tooling.
	covResult := coverage.Detect(".")
	var covInput *scorer.CoverageInput
	if covResult.Coverage >= 0 {
		covInput = &scorer.CoverageInput{
			Coverage: covResult.Coverage,
			Source:   string(covResult.Source),
		}
	}

	logx.Debugf("check: detection ai_generated=%t confidence=%.2f sources=%v",
		detectResult.AIGenerated, detectResult.Confidence, detectResult.Sources)
	logx.Debugf("check: analysis issues=%d (changed lines=%d, test lines=%d)",
		len(analyzeResult.Issues), totalLines, testLines)
	logx.Debugf("check: coverage source=%s value=%.2f", covResult.Source, covResult.Coverage)

	scoreResult := scorer.Score(scorer.Options{
		DetectorResult:    detectResult,
		AnalyzerResult:    analyzeResult,
		TestLines:         testLines,
		TotalChangedLines: totalLines,
		CoverageResult:    covInput,
	})

	logx.Debugf("check: score delta=%.2f verdict=%s (ai_ratio=%.2f defect_density=%.2f critical=%d coverage=%.2f dup=%.2f)",
		scoreResult.TechnicalDebtDelta, scoreResult.Verdict,
		scoreResult.Breakdown.AICodeRatio, scoreResult.Breakdown.DefectDensity,
		scoreResult.Breakdown.CriticalIssues, scoreResult.Breakdown.TestCoverage,
		scoreResult.Breakdown.DuplicationRate)

	// Build complete list of changed file paths for policy input.
	// getAllChangedFiles includes all file types (not just code files), so Rego
	// rules like `endswith(input.changed_files[_], ".go")` work correctly.
	changedFiles, _ := getAllChangedFiles(diffBase)
	if len(changedFiles) == 0 {
		// Fallback: use keys from code-file diff when git is unavailable
		for f := range diffFiles {
			changedFiles = append(changedFiles, f)
		}
	}

	// Deterministic merge-confidence signals from the diff (tested? shaped like
	// real work? touches sensitive paths?). Reuses the per-code-file added-line
	// counts already computed for the analyzer.
	addedByCodeFile := make(map[string]int, len(diffFiles))
	for f, lines := range diffFiles {
		addedByCodeFile[f] = len(lines)
	}
	mc := mergeconf.Compute(changedFiles, addedByCodeFile)
	logx.Debugf("check: merge-confidence tests_touched=%t added_source_without_tests=%t risky_paths=%d files=%d",
		mc.TestsTouched, mc.AddedSourceWithoutTests, len(mc.RiskyPaths), mc.FilesChanged)

	// Build policy input
	evalInput := &policy.EvalInput{
		AIGenerated:        detectResult.AIGenerated,
		AIConfidence:       detectResult.Confidence,
		DetectionSources:   detectResult.Sources,
		TechnicalDebtDelta: scoreResult.TechnicalDebtDelta,
		TestCoverage:       scoreResult.Breakdown.TestCoverage,
		TestCoverageSource: scoreResult.Breakdown.TestCoverageSource,
		Branch:             detectOpts.BranchName,
		ChangedFiles:       changedFiles,
		MergeConfidence: &policy.EvalMergeConfidence{
			FilesChanged:            mc.FilesChanged,
			SourceFilesChanged:      mc.SourceFilesChanged,
			TestFilesChanged:        mc.TestFilesChanged,
			NetAddedLines:           mc.NetAddedLines,
			TestsTouched:            mc.TestsTouched,
			AddedSourceWithoutTests: mc.AddedSourceWithoutTests,
			RiskyPaths:              mc.RiskyPaths,
		},
		// _ods_detect_error is not set here — it's added by validate-action when
		// the detect stage fails, not by the CLI's check command.
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

	// Attach AI reviewer verdicts. Verdicts stamped for a different commit are
	// stale opinions and are skipped with a warning — they must not enter the
	// gate. Load errors are also non-fatal: a malformed advisory input must
	// not break the deterministic gate.
	if len(checkAIReviews) > 0 {
		headSHA := gitHeadSHA()
		for _, path := range checkAIReviews {
			v, err := review.Load(path)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: skipping AI review: %v\n", err)
				continue
			}
			if !v.MatchesHead(headSHA) {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"Warning: skipping AI review %s: verdict is for commit %s, HEAD is %s\n",
					path, v.HeadSHA, headSHA)
				continue
			}
			ar := policy.EvalAIReview{
				Tool:    v.Reviewer.Tool,
				Model:   v.Reviewer.Model,
				Verdict: v.Verdict,
			}
			for _, f := range v.Findings {
				ar.Findings = append(ar.Findings, policy.EvalReviewIssue{
					File:     f.File,
					Line:     f.Line,
					Severity: f.Severity,
					Category: f.Category,
					Message:  f.Message,
				})
			}
			evalInput.AIReviews = append(evalInput.AIReviews, ar)
			logx.Debugf("check: attached AI review from %s (%s, %d finding(s))",
				v.Reviewer.Tool, v.Verdict, len(v.Findings))
		}
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

	logx.Debugf("check: policy result allowed=%t denials=%d warnings=%d",
		result.Allowed, len(result.Denials), len(result.Warnings))
	for _, d := range result.Denials {
		logx.Debugf("check: DENY %s", d)
	}
	for _, w := range result.Warnings {
		logx.Debugf("check: WARN %s", w)
	}

	switch {
	case checkJSON:
		// Echo the deterministic merge-confidence facts the gate saw alongside
		// the result, so reports and auditors can render them without re-running
		// the pipeline. Embeds EvalResult so its fields stay top-level.
		out := struct {
			*policy.EvalResult
			MergeConfidence *policy.EvalMergeConfidence `json:"merge_confidence,omitempty"`
		}{EvalResult: result, MergeConfidence: evalInput.MergeConfidence}
		data, err := json.MarshalIndent(out, "", "  ")
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

	// Review tier only routes changes that may merge; a blocked PR needs a fix,
	// not a reviewer assignment.
	if result.ReviewTier != "" && result.Allowed {
		fmt.Fprintf(cmd.OutOrStdout(), "   Review tier: %s\n", result.ReviewTier)
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

// gitHeadSHA returns the commit that review verdicts are matched against.
// ODS_HEAD_SHA wins when set: CI checks out a synthetic merge commit on
// pull_request events, so `git rev-parse HEAD` is not the SHA reviewers
// stamped into their verdicts. Falls back to HEAD, or "" outside a git repo
// (callers treat the empty value as "nothing to compare against").
func gitHeadSHA() string {
	if sha := os.Getenv("ODS_HEAD_SHA"); sha != "" {
		return sha
	}
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
