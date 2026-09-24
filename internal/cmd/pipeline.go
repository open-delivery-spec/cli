package cmd

import (
	"fmt"

	"github.com/open-delivery-spec/cli/internal/analyzer"
	"github.com/open-delivery-spec/cli/internal/coverage"
	"github.com/open-delivery-spec/cli/internal/detector"
	"github.com/open-delivery-spec/cli/internal/logx"
	"github.com/open-delivery-spec/cli/internal/sarif"
	"github.com/open-delivery-spec/cli/internal/scorer"
	"github.com/spf13/cobra"
)

// pipelineRun is what detect → analyze → score produce for one diff range.
type pipelineRun struct {
	Base       string
	Branch     string
	Detect     *detector.DetectionResult
	Analysis   *analyzer.AnalysisResult
	DiffFiles  map[string][]string // added lines per changed code file
	TotalLines int                 // added lines across every changed code file
	Coverage   coverage.Result
	Score      *scorer.ScoreResult
}

// runPipeline runs detect → analyze → score over diffBase once, the same way
// for `ods score`, `ods check` and `ods attest`, so the number one command
// prints is the number the gate acted on and the evidence document records.
// sarifPath merges an external scanner's findings; coverageFile names a
// coverage report, "" auto-detects one in the working directory.
func runPipeline(cmd *cobra.Command, diffBase, sarifPath, coverageFile string) pipelineRun {
	p := pipelineRun{Base: diffBase}

	detectOpts := detector.Options{DiffBase: diffBase, MaxCommits: 10}
	// The branch and the PR description resolve exactly as in `ods detect`,
	// so a disclosure the detect stage sees also reaches the policy input.
	p.Branch = resolveBranch(detectBranch)
	detectOpts.BranchName = p.Branch
	if body, err := resolvePRBody(detectPRBody, detectPRFile); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", err)
	} else {
		detectOpts.PRBody = body
	}
	detectResult, err := detector.Detect(detectOpts)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: detection failed: %v\n", err)
		detectResult = &detector.DetectionResult{}
	}
	p.Detect = detectResult

	p.DiffFiles, _ = getGitDiffFiles(diffBase)
	if len(p.DiffFiles) > 0 {
		p.Analysis = analyzer.Analyze(analyzer.Options{Files: p.DiffFiles})
	} else {
		p.Analysis = &analyzer.AnalysisResult{}
	}

	// External findings make the gate and the score act on authoritative
	// analyzer results, not just the built-in heuristics.
	if sarifPath != "" {
		if iss, err := sarif.Load(sarifPath); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: loading SARIF file: %v\n", err)
		} else {
			p.Analysis.Issues = append(p.Analysis.Issues, iss...)
			p.Analysis.Summary = analyzer.ResummarizeSARIF(p.Analysis.Issues)
			logx.Debugf("pipeline: merged %d SARIF finding(s) from %s", len(iss), sarifPath)
		}
	}

	// The AI-ratio denominator: added lines across every changed code file.
	// Summing only the AI-detected files made any AI-touched change look
	// 100% AI.
	for _, lines := range p.DiffFiles {
		p.TotalLines += len(lines)
	}
	if p.TotalLines == 0 {
		for _, f := range detectResult.Files {
			p.TotalLines += f.TotalLines
		}
	}
	if p.TotalLines == 0 && p.Analysis.TotalLines > 0 {
		p.TotalLines = p.Analysis.TotalLines
	}
	if p.TotalLines == 0 {
		p.TotalLines = 1
	}

	// Coverage comes from a report or is "not measured" (-1); the scorer then
	// skips the coverage-gap term rather than guessing.
	if coverageFile != "" {
		p.Coverage = coverage.Parse(coverageFile)
	} else {
		p.Coverage = coverage.Detect(".")
	}
	var covInput *scorer.CoverageInput
	if p.Coverage.Coverage >= 0 {
		covInput = &scorer.CoverageInput{
			Coverage: p.Coverage.Coverage,
			Source:   string(p.Coverage.Source),
		}
	}

	logx.Debugf("pipeline: detection ai_generated=%t confidence=%.2f sources=%v",
		detectResult.AIGenerated, detectResult.Confidence, detectResult.Sources)
	logx.Debugf("pipeline: analysis issues=%d over %d changed line(s)", len(p.Analysis.Issues), p.TotalLines)
	logx.Debugf("pipeline: coverage source=%s value=%.2f", p.Coverage.Source, p.Coverage.Coverage)

	p.Score = scorer.Score(scorer.Options{
		DetectorResult:    detectResult,
		AnalyzerResult:    p.Analysis,
		TotalChangedLines: p.TotalLines,
		CoverageResult:    covInput,
		DiffBase:          diffBase,
	})
	logx.Debugf("pipeline: score delta=%.2f verdict=%s risk=%s (ai_ratio=%.2f/%s defect_density=%.2f critical=%d coverage=%.2f dup=%.2f)",
		p.Score.TechnicalDebtDelta, p.Score.Verdict, p.Score.Risk,
		p.Score.Breakdown.AICodeRatio, p.Score.Breakdown.AICodeRatioSource, p.Score.Breakdown.DefectDensity,
		p.Score.Breakdown.CriticalIssues, p.Score.Breakdown.TestCoverage, p.Score.Breakdown.DuplicationRate)
	return p
}
