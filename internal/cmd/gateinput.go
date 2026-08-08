package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/open-delivery-spec/cli/internal/analyzer"
	"github.com/open-delivery-spec/cli/internal/coverage"
	"github.com/open-delivery-spec/cli/internal/detector"
	"github.com/open-delivery-spec/cli/internal/logx"
	"github.com/open-delivery-spec/cli/internal/mergeconf"
	"github.com/open-delivery-spec/cli/internal/mutation"
	"github.com/open-delivery-spec/cli/internal/policy"
	"github.com/open-delivery-spec/cli/internal/review"
	"github.com/open-delivery-spec/cli/internal/sarif"
	"github.com/open-delivery-spec/cli/internal/scorer"
	"github.com/spf13/cobra"
)

// assembleGateInputs runs the detect → analyze → score pipeline over diffBase
// and assembles the complete policy input, exactly as `ods check` evaluates
// it. Shared by check (the gate) and attest (the evidence document) so both
// act on the same facts — the evidence document must never be assembled from
// a different pipeline than the one that gated the change.
func assembleGateInputs(cmd *cobra.Command, diffBase, sarifPath string, aiReviews []string, mutationPath string) (*policy.EvalInput, *detector.DetectionResult, error) {
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
	if sarifPath != "" {
		if iss, err := sarif.Load(sarifPath); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: loading SARIF file: %v\n", err)
		} else {
			analyzeResult.Issues = append(analyzeResult.Issues, iss...)
			analyzeResult.Summary = analyzer.ResummarizeSARIF(analyzeResult.Issues)
			logx.Debugf("check: merged %d SARIF finding(s) from %s", len(iss), sarifPath)
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

	// Patch (diff) coverage: of the added lines, how many are covered by tests?
	// Requires a per-line coverage report; −1 ("not measured") otherwise.
	patchCoverage := coverage.NotMeasured
	if hits, src, ok := coverage.DetectLines("."); ok {
		if added, err := getDiffAddedLineNumbers(diffBase); err == nil {
			if cov, tot := coverage.PatchCoverage(added, hits); tot > 0 {
				patchCoverage = float64(cov) / float64(tot)
				logx.Debugf("check: patch coverage %.2f (%d/%d added lines, source=%s)", patchCoverage, cov, tot, src)
			}
		}
	}

	// Mutation score (diff-scoped): of the mutants on the added lines, how many
	// did the tests kill? Ingested from a gremlins report via --mutation; −1
	// ("not measured") when no report is given or no mutant lands on a change.
	mutationScore := mutation.NotMeasured
	if mutationPath != "" {
		report, err := mutation.Parse(mutationPath)
		if err != nil {
			return nil, nil, err
		}
		if added, err := getDiffAddedLineNumbers(diffBase); err == nil {
			if killed, tot := report.DiffScopedMSI(added); tot > 0 {
				mutationScore = float64(killed) / float64(tot)
				logx.Debugf("check: mutation score %.2f (%d/%d mutants on added lines)", mutationScore, killed, tot)
			}
		}
	}

	// Build policy input
	evalInput := &policy.EvalInput{
		AIGenerated:        detectResult.AIGenerated,
		AIConfidence:       detectResult.Confidence,
		DetectionSources:   detectResult.Sources,
		EvidenceTier:       detector.EvidenceTier(detectResult.Sources),
		TechnicalDebtDelta: scoreResult.TechnicalDebtDelta,
		TestCoverage:       scoreResult.Breakdown.TestCoverage,
		TestCoverageSource: scoreResult.Breakdown.TestCoverageSource,
		Branch:             detectOpts.BranchName,
		ChangedFiles:       changedFiles,
		PatchCoverage:      patchCoverage,
		MutationScore:      mutationScore,
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
	if len(aiReviews) > 0 {
		headSHA := gitHeadSHA()
		for _, path := range aiReviews {
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

	return evalInput, detectResult, nil
}
