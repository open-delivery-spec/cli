package scorer

import (
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
		TestLines:         80,
		TotalChangedLines: 100,
	})

	if result.Verdict != "decrease" {
		t.Errorf("verdict = %s, want decrease", result.Verdict)
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
		TestLines:         10,
		TotalChangedLines: 100,
	})

	if result.Verdict != "increase" {
		t.Errorf("verdict = %s, want increase", result.Verdict)
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
		TestLines:         50,
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
		TestLines:         20,
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
		TestLines:         90,
		TotalChangedLines: 100,
	})
	if res.Breakdown.AICodeRatio != 1.0 {
		t.Fatalf("AI ratio = %f, want 1.0", res.Breakdown.AICodeRatio)
	}
	if res.Verdict != "decrease" {
		t.Errorf("clean 100%% AI PR should be low risk, got verdict %q (delta %f)",
			res.Verdict, res.TechnicalDebtDelta)
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
			TestLines:         100, // coverage 1.0 → no coverage-gap term
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
		TestLines:         30,
		TotalChangedLines: 100,
	})

	formatted := result.FormatScore()
	if !strings.Contains(formatted, "Tech Debt Delta") {
		t.Errorf("FormatScore missing header: %s", formatted)
	}
}

func TestEstimateDuplication(t *testing.T) {
	rate := estimateDuplication()
	// Should return a number between 0 and 1 (or 0 if no git repo)
	if rate < 0 || rate > 1 {
		t.Errorf("duplication rate = %f, want 0.0-1.0", rate)
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
