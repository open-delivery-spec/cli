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

func TestTrend(t *testing.T) {
	points := []TrendPoint{
		{TechnicalDebtDelta: 2.0, IsAIPR: true},
		{TechnicalDebtDelta: 3.0, IsAIPR: true},
		{TechnicalDebtDelta: -0.5, IsAIPR: false},
		{TechnicalDebtDelta: 0.3, IsAIPR: false},
	}

	summary := Trend(points)
	if !strings.Contains(summary, "AI PRs avg") || !strings.Contains(summary, "Human PRs avg") {
		t.Errorf("Trend summary = %s, missing avg data", summary)
	}
}

func TestTrendEmpty(t *testing.T) {
	summary := Trend(nil)
	if summary != "No data available" {
		t.Errorf("empty trend = %s, want 'No data available'", summary)
	}
}

func TestEstimateDuplication(t *testing.T) {
	rate := estimateDuplication()
	// Should return a number between 0 and 1 (or 0 if no git repo)
	if rate < 0 || rate > 1 {
		t.Errorf("duplication rate = %f, want 0.0-1.0", rate)
	}
}
