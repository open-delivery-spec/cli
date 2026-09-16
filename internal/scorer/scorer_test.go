package scorer

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-delivery-spec/cli/internal/analyzer"
	"github.com/open-delivery-spec/cli/internal/detector"
)

func TestScoreLowRisk(t *testing.T) {
	result := Score(Options{
		DetectorResult: &detector.DetectionResult{
			AIGenerated: false,
			Confidence:  0,
			Files:       nil,
		},
		AnalyzerResult: &analyzer.AnalysisResult{
			TotalLines: 100,
			Issues:     nil,
		},
		CoverageResult:    &CoverageInput{Coverage: 0.8, Source: "go"},
		TotalChangedLines: 100,
	})

	if result.Risk != "low" {
		t.Errorf("risk = %s, want low", result.Risk)
	}
	if result.TechnicalDebtDelta > 1.0 {
		t.Errorf("delta = %f, want <= 1.0 for low-risk", result.TechnicalDebtDelta)
	}
}

func TestScoreHighRisk(t *testing.T) {
	result := Score(Options{
		DetectorResult: &detector.DetectionResult{
			AIGenerated: true,
			Confidence:  0.85,
			Files: []detector.FileDetection{
				{Path: "auth.go", AILines: 60, TotalLines: 100, Confidence: 0.8},
			},
		},
		AnalyzerResult: &analyzer.AnalysisResult{
			TotalLines: 100,
			Issues: []analyzer.Issue{
				{Rule: "test", Severity: "critical", Line: 1},
				{Rule: "test", Severity: "high", Line: 2},
				{Rule: "test", Severity: "medium", Line: 3},
			},
		},
		CoverageResult:    &CoverageInput{Coverage: 0.1, Source: "go"},
		TotalChangedLines: 100,
	})

	if result.Verdict != "increase" {
		t.Errorf("verdict = %s, want increase", result.Verdict)
	}
	if result.Risk != "high" && result.Risk != "critical" {
		t.Errorf("risk = %s, want high or critical", result.Risk)
	}
	if result.TechnicalDebtDelta < 3.0 {
		t.Errorf("delta = %f, want >= 3.0 for high-risk", result.TechnicalDebtDelta)
	}
}

func TestScoreNeutral(t *testing.T) {
	result := Score(Options{
		DetectorResult: &detector.DetectionResult{
			AIGenerated: true,
			Confidence:  0.5,
			Files: []detector.FileDetection{
				{Path: "util.go", AILines: 20, TotalLines: 100, Confidence: 0.4},
			},
		},
		AnalyzerResult: &analyzer.AnalysisResult{
			TotalLines: 100,
			Issues: []analyzer.Issue{
				{Rule: "test", Severity: "low", Line: 1},
			},
		},
		CoverageResult:    &CoverageInput{Coverage: 0.5, Source: "go"},
		TotalChangedLines: 100,
	})

	// With moderate AI ratio (0.2), 1 low issue, and 50% test coverage,
	// tech debt delta should be reasonable (not extreme)
	b := result.Breakdown
	if b.AICodeRatio != 0.2 {
		t.Errorf("AI ratio = %f, want 0.2", b.AICodeRatio)
	}
	if b.TestCoverage != 0.5 {
		t.Errorf("test coverage = %f, want 0.5", b.TestCoverage)
	}
	// Verdict should be at most "increase" (not some weird value)
	if result.Verdict != "decrease" && result.Verdict != "neutral" && result.Verdict != "increase" {
		t.Errorf("verdict = %s, want one of: decrease, neutral, increase", result.Verdict)
	}
}

func TestScoreBreakdown(t *testing.T) {
	result := Score(Options{
		DetectorResult: &detector.DetectionResult{
			AIGenerated: true,
			Confidence:  0.9,
			Files: []detector.FileDetection{
				{Path: "a.go", AILines: 40, TotalLines: 100, Confidence: 0.8},
			},
		},
		AnalyzerResult: &analyzer.AnalysisResult{
			TotalLines: 100,
			Issues: []analyzer.Issue{
				{Rule: "t1", Severity: "critical", Line: 1},
				{Rule: "t2", Severity: "high", Line: 2},
				{Rule: "t3", Severity: "high", Line: 3},
			},
		},
		CoverageResult:    &CoverageInput{Coverage: 0.2, Source: "go"},
		TotalChangedLines: 100,
	})

	b := result.Breakdown
	if b.AICodeRatio != 0.4 {
		t.Errorf("AI ratio = %f, want 0.4", b.AICodeRatio)
	}
	if b.CriticalIssues != 3 {
		t.Errorf("critical issues = %d, want 3", b.CriticalIssues)
	}
	if b.TestCoverage != 0.2 {
		t.Errorf("test coverage = %f, want 0.2", b.TestCoverage)
	}
}

// TestScore_CleanAIPRIsLowRisk locks the core of the quality-driven model: a
// fully AI-written change with no defects and good coverage must score ~0. AI
// quantity alone must never create technical debt.
func TestScore_CleanAIPRIsLowRisk(t *testing.T) {
	t.Setenv("ODS_DIFF_BASE", "HEAD") // diff against HEAD → zero duplication, deterministic
	res := Score(Options{
		DetectorResult: &detector.DetectionResult{
			AIGenerated: true, Confidence: 1.0,
			Files: []detector.FileDetection{
				{Path: "a.go", AILines: 100, TotalLines: 100, Confidence: 1.0},
			},
		},
		AnalyzerResult:    &analyzer.AnalysisResult{TotalLines: 100, Issues: nil},
		CoverageResult:    &CoverageInput{Coverage: 0.9, Source: "go"},
		TotalChangedLines: 100,
	})
	if res.Breakdown.AICodeRatio != 1.0 {
		t.Fatalf("AI ratio = %f, want 1.0", res.Breakdown.AICodeRatio)
	}
	if res.Risk != "low" {
		t.Errorf("clean 100%% AI PR should be low risk, got risk %q (delta %f)",
			res.Risk, res.TechnicalDebtDelta)
	}
}

// TestScore_NoFileDataIsNotGuessed locks the fix for the invented AI ratio:
// with no per-file attribution the scorer used to report
// changed_lines × confidence × 0.5 — "49%" for a change whose every commit
// was attested AI. No data means no ratio, marked as not measured.
func TestScore_NoFileDataIsNotGuessed(t *testing.T) {
	t.Setenv("ODS_DIFF_BASE", "HEAD")
	res := Score(Options{
		DetectorResult:    &detector.DetectionResult{AIGenerated: true, Confidence: 1.0, Sources: []string{"commit-trailer"}},
		AnalyzerResult:    &analyzer.AnalysisResult{TotalLines: 100},
		TotalChangedLines: 100,
	})
	if res.Breakdown.AICodeRatio != 0 {
		t.Errorf("AI ratio = %f, want 0: nothing measured means nothing reported", res.Breakdown.AICodeRatio)
	}
	if res.Breakdown.AICodeRatioSource != "unknown" {
		t.Errorf("ratio source = %q, want unknown", res.Breakdown.AICodeRatioSource)
	}
}

// TestScore_RatioSourceFollowsDetector: the ratio carries the provenance of
// the per-file counts it was computed from, strongest source first, and is
// capped at 1 so a denominator narrower than the numerator cannot exceed it.
func TestScore_RatioSourceFollowsDetector(t *testing.T) {
	t.Setenv("ODS_DIFF_BASE", "HEAD")
	mk := func(sources ...string) *ScoreResult {
		return Score(Options{
			DetectorResult: &detector.DetectionResult{
				AIGenerated: true, Confidence: 0.9, Sources: sources,
				Files: []detector.FileDetection{{Path: "a.go", AILines: 150, TotalLines: 150, Confidence: 0.9}},
			},
			AnalyzerResult:    &analyzer.AnalysisResult{TotalLines: 100},
			TotalChangedLines: 100,
		})
	}
	cases := []struct {
		sources []string
		want    string
	}{
		{[]string{"commit-trailer", "branch-name"}, "commit-trailer"},
		{[]string{"git-ai-notes", "commit-trailer"}, "git-ai"},
		{[]string{"branch-name", "diff-heuristics"}, "diff-heuristics"},
		{[]string{"pr-body"}, "unknown"},
	}
	for _, c := range cases {
		res := mk(c.sources...)
		if res.Breakdown.AICodeRatioSource != c.want {
			t.Errorf("sources %v: ratio source = %q, want %q", c.sources, res.Breakdown.AICodeRatioSource, c.want)
		}
		if res.Breakdown.AICodeRatio != 1 {
			t.Errorf("sources %v: ratio = %f, want capped at 1", c.sources, res.Breakdown.AICodeRatio)
		}
	}
}

// TestScore_VerdictIsDirectionRiskIsBand: verdict says which way the delta
// points, risk says how far. +0.1 used to be labelled "decrease".
func TestScore_VerdictIsDirectionRiskIsBand(t *testing.T) {
	t.Setenv("ODS_DIFF_BASE", "HEAD") // zero duplication, deterministic
	mk := func(coverage float64, issues []analyzer.Issue) *ScoreResult {
		return Score(Options{
			DetectorResult:    &detector.DetectionResult{},
			AnalyzerResult:    &analyzer.AnalysisResult{TotalLines: 100, Issues: issues},
			CoverageResult:    &CoverageInput{Coverage: coverage, Source: "go"},
			TotalChangedLines: 100,
		})
	}
	high := func(n int) []analyzer.Issue {
		var iss []analyzer.Issue
		for i := 0; i < n; i++ {
			iss = append(iss, analyzer.Issue{Rule: "t", Severity: "high", Line: i + 1})
		}
		return iss
	}
	cases := []struct {
		name          string
		res           *ScoreResult
		verdict, risk string
	}{
		{"fully covered, no issues → +0.0", mk(1.0, nil), "neutral", "low"},
		{"90% coverage → +0.1 is an increase, still low risk", mk(0.9, nil), "increase", "low"},
		{"one high finding + gap → moderate", mk(0.5, high(1)), "increase", "moderate"},
		{"three high findings → high", mk(1.0, high(3)), "increase", "high"},
		{"four high findings → critical", mk(1.0, high(4)), "increase", "critical"},
	}
	for _, c := range cases {
		if c.res.Verdict != c.verdict || c.res.Risk != c.risk {
			t.Errorf("%s: verdict=%q risk=%q (delta %.2f), want verdict=%q risk=%q",
				c.name, c.res.Verdict, c.res.Risk, c.res.TechnicalDebtDelta, c.verdict, c.risk)
		}
	}
}

// TestScore_AIRatioAmplifiesButDoesNotCreate verifies AI ratio is a bounded
// multiplier on real quality debt, not a standalone debt source: the same
// defect produces debt regardless of authorship, and AI only amplifies it
// (by at most 1.5x).
func TestScore_AIRatioAmplifiesButDoesNotCreate(t *testing.T) {
	t.Setenv("ODS_DIFF_BASE", "HEAD") // zero duplication, deterministic
	mk := func(aiLines int) *ScoreResult {
		return Score(Options{
			DetectorResult: &detector.DetectionResult{
				AIGenerated: aiLines > 0, Confidence: 1.0,
				Files: []detector.FileDetection{
					{Path: "a.go", AILines: aiLines, TotalLines: 100, Confidence: 1.0},
				},
			},
			AnalyzerResult: &analyzer.AnalysisResult{
				TotalLines: 100,
				Issues:     []analyzer.Issue{{Rule: "t", Severity: "high", Line: 1}},
			},
			CoverageResult:    &CoverageInput{Coverage: 1.0, Source: "go"}, // no coverage-gap term
			TotalChangedLines: 100,
		})
	}
	human := mk(0) // AI ratio 0
	ai := mk(100)  // AI ratio 1.0

	if human.TechnicalDebtDelta <= 0 {
		t.Errorf("a real high-severity issue must produce debt regardless of AI, got %f",
			human.TechnicalDebtDelta)
	}
	if ai.TechnicalDebtDelta <= human.TechnicalDebtDelta {
		t.Errorf("AI ratio should amplify quality debt: ai=%f human=%f",
			ai.TechnicalDebtDelta, human.TechnicalDebtDelta)
	}
	if ai.TechnicalDebtDelta > human.TechnicalDebtDelta*1.5+1e-9 {
		t.Errorf("amplification must be bounded at 1.5x: ai=%f human=%f",
			ai.TechnicalDebtDelta, human.TechnicalDebtDelta)
	}
}

func TestFormatScore(t *testing.T) {
	result := Score(Options{
		DetectorResult: &detector.DetectionResult{
			AIGenerated: true,
			Confidence:  0.9,
			Files: []detector.FileDetection{
				{Path: "a.go", AILines: 30, TotalLines: 100, Confidence: 0.8},
			},
		},
		AnalyzerResult: &analyzer.AnalysisResult{
			TotalLines: 100,
			Issues: []analyzer.Issue{
				{Rule: "t1", Severity: "critical", Line: 1},
			},
		},
		CoverageResult:    &CoverageInput{Coverage: 0.3, Source: "go"},
		TotalChangedLines: 100,
	})

	formatted := result.FormatScore()
	if !strings.Contains(formatted, "Tech Debt Delta") {
		t.Errorf("FormatScore missing header: %s", formatted)
	}
}

func TestEstimateDuplication(t *testing.T) {
	rate := estimateDuplication("")
	// Should return a number between 0 and 1 (or 0 if no git repo)
	if rate < 0 || rate > 1 {
		t.Errorf("duplication rate = %f, want 0.0-1.0", rate)
	}
}

// gitIn runs a git command in dir with a fixed identity and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// TestEstimateDuplication_codeFilesOnly: repeated lines in a README are not
// copy-pasted code. A docs-only change estimates 0; the same repetition in a
// code file counts.
func TestEstimateDuplication_codeFilesOnly(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "chore: init")
	t.Chdir(dir)
	t.Setenv("ODS_DIFF_BASE", "HEAD")

	repeated := strings.Repeat("| a table row that repeats itself | yes |\n", 4)
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte(repeated), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	if rate := estimateDuplication("HEAD"); rate != 0 {
		t.Errorf("docs-only change: duplication = %f, want 0", rate)
	}

	code := "package p\n\n" + strings.Repeat("\tcallSomething(withArgument)\n", 4)
	if err := os.WriteFile(filepath.Join(dir, "dup.go"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	if rate := estimateDuplication("HEAD"); rate <= 0 {
		t.Errorf("repeated code lines: duplication = %f, want > 0", rate)
	}
}

func TestIsStructuralLine(t *testing.T) {
	structural := []string{
		"", "   ", "// a comment", "# py comment", "/* block */", "* doc",
		"}", "})", "},", ")", "]", "{", "});", "return nil", "break",
		"continue", "default:", "x := 1",
	}
	for _, s := range structural {
		if !isStructuralLine(strings.TrimSpace(s)) {
			t.Errorf("isStructuralLine(%q) = false, want true", s)
		}
	}

	meaningful := []string{
		"total += item.Price * item.Quantity",
		"result, err := process(ctx, data)",
		"if err != nil { return fmt.Errorf(\"x: %w\", err) }",
	}
	for _, s := range meaningful {
		if isStructuralLine(strings.TrimSpace(s)) {
			t.Errorf("isStructuralLine(%q) = true, want false", s)
		}
	}
}

func TestDuplicationRate(t *testing.T) {
	t.Run("structural repetition does not count", func(t *testing.T) {
		// All closing braces / trivial returns: previously inflated to a high
		// rate; must now be 0 because none are meaningful lines.
		lines := []string{
			"func a() error {", "}", "func b() error {", "}",
			"return nil", "return nil", "}", "}",
		}
		if r := duplicationRate(lines); r != 0 {
			t.Errorf("duplicationRate = %f, want 0 (structural lines excluded)", r)
		}
	})

	t.Run("real duplicated lines count", func(t *testing.T) {
		dup := "user.Roles = append(user.Roles, adminRole)"
		lines := []string{dup, "x := compute(alpha, beta)", dup, dup}
		// 4 meaningful lines counted, dup appears 3x → 2 duplicates / 4 total.
		got := duplicationRate(lines)
		want := 2.0 / 4.0
		if got < want-0.001 || got > want+0.001 {
			t.Errorf("duplicationRate = %f, want ~%f", got, want)
		}
	})

	t.Run("no added lines", func(t *testing.T) {
		if r := duplicationRate(nil); r != 0 {
			t.Errorf("duplicationRate(nil) = %f, want 0", r)
		}
	})
}

// TestScore_DeltaIndependentOfDiffSize locks the fix for the density
// explosion: one high finding must cost the same debt in a tiny diff as in a
// large one. Density is a per-KLOC ratio — charging it into the delta made
// the same finding score 100x higher on a 20-line change than on a 2000-line
// change, blocking small PRs hardest (the opposite of the real risk) and
// double-charging findings already counted absolutely.
func TestScore_DeltaIndependentOfDiffSize(t *testing.T) {
	t.Setenv("ODS_DIFF_BASE", "HEAD") // zero duplication, deterministic
	mk := func(lines int) *ScoreResult {
		return Score(Options{
			AnalyzerResult: &analyzer.AnalysisResult{
				TotalLines: lines,
				Issues:     []analyzer.Issue{{Rule: "t", Severity: "high", Line: 1}},
			},
			CoverageResult:    &CoverageInput{Coverage: 1.0, Source: "go"}, // no coverage-gap term
			TotalChangedLines: lines,
		})
	}
	small, large := mk(20), mk(2000)
	if math.Abs(small.TechnicalDebtDelta-large.TechnicalDebtDelta) > 1e-9 {
		t.Errorf("delta depends on diff size: 20 lines → %f, 2000 lines → %f",
			small.TechnicalDebtDelta, large.TechnicalDebtDelta)
	}
	// The ratio itself stays visible in the breakdown for humans.
	if small.Breakdown.DefectDensity <= large.Breakdown.DefectDensity {
		t.Errorf("density should still reflect the per-KLOC ratio: small=%f large=%f",
			small.Breakdown.DefectDensity, large.Breakdown.DefectDensity)
	}
}

// TestScore_CoverageIsNeverEstimated: without a parsed coverage report the
// coverage is "not measured" (-1) and the coverage-gap term is skipped. It used
// to be estimated as test-file lines over changed lines, a number that is not
// coverage and could exceed 100%.
func TestScore_CoverageIsNeverEstimated(t *testing.T) {
	res := Score(Options{
		DetectorResult:    &detector.DetectionResult{},
		AnalyzerResult:    &analyzer.AnalysisResult{TotalLines: 100},
		TotalChangedLines: 100,
		DiffBase:          "HEAD",
	})
	if res.Breakdown.TestCoverage != -1 || res.Breakdown.TestCoverageSource != "unknown" {
		t.Errorf("coverage = %f (%s), want -1 (unknown)", res.Breakdown.TestCoverage, res.Breakdown.TestCoverageSource)
	}
	if res.TechnicalDebtDelta != 0 {
		t.Errorf("delta = %f, want 0 with nothing measured", res.TechnicalDebtDelta)
	}
}
