package report

import (
	"math"
	"testing"
	"time"

	"github.com/open-delivery-spec/cli/internal/ledger"
)

func ts(day int) time.Time {
	return time.Date(2026, 1, day, 12, 0, 0, 0, time.UTC)
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestComputeTrendsEmpty(t *testing.T) {
	tr := ComputeTrends(nil)
	if tr.Runs != 0 {
		t.Errorf("Runs = %d, want 0", tr.Runs)
	}
	if tr.Summary != "No ledger history yet" {
		t.Errorf("Summary = %q", tr.Summary)
	}
	// Sentinels for "nothing measured".
	if tr.FirstCoverage != -1 || tr.LatestCoverage != -1 {
		t.Errorf("coverage sentinels = %v/%v, want -1/-1", tr.FirstCoverage, tr.LatestCoverage)
	}
	if tr.AIDefectDensity != -1 || tr.HumanDefectDensity != -1 {
		t.Errorf("defect sentinels = %v/%v, want -1/-1", tr.AIDefectDensity, tr.HumanDefectDensity)
	}
}

func TestComputeTrendsAggregates(t *testing.T) {
	recs := []ledger.Record{
		{Timestamp: ts(1), TechnicalDebtDelta: 2, TestCoverage: 0.60, DefectDensity: 1.0, AIGenerated: true},
		{Timestamp: ts(2), TechnicalDebtDelta: 6, TestCoverage: -1, DefectDensity: 4.0, AIGenerated: true},  // high-risk, coverage not measured
		{Timestamp: ts(3), TechnicalDebtDelta: 1, TestCoverage: 0.80, DefectDensity: 0.5, AIGenerated: false},
	}
	tr := ComputeTrends(recs)

	if tr.Runs != 3 {
		t.Errorf("Runs = %d, want 3", tr.Runs)
	}
	if !approx(tr.NetDebtDelta, 9) {
		t.Errorf("NetDebtDelta = %v, want 9", tr.NetDebtDelta)
	}
	if !approx(tr.AvgDebtDelta, 3) {
		t.Errorf("AvgDebtDelta = %v, want 3", tr.AvgDebtDelta)
	}
	if tr.HighRiskRuns != 1 {
		t.Errorf("HighRiskRuns = %d, want 1", tr.HighRiskRuns)
	}
	if !approx(tr.HighRiskRate, 1.0/3.0) {
		t.Errorf("HighRiskRate = %v, want 1/3", tr.HighRiskRate)
	}
	// Coverage trend only over measured runs (day 1 and day 3).
	if tr.CoverageRuns != 2 {
		t.Errorf("CoverageRuns = %d, want 2", tr.CoverageRuns)
	}
	if !approx(tr.FirstCoverage, 0.60) || !approx(tr.LatestCoverage, 0.80) {
		t.Errorf("coverage = %v → %v, want 0.60 → 0.80", tr.FirstCoverage, tr.LatestCoverage)
	}
	if !approx(tr.CoverageDelta, 0.20) {
		t.Errorf("CoverageDelta = %v, want 0.20", tr.CoverageDelta)
	}
	// AI vs human defect density.
	if tr.AIRuns != 2 || tr.HumanRuns != 1 {
		t.Errorf("AIRuns/HumanRuns = %d/%d, want 2/1", tr.AIRuns, tr.HumanRuns)
	}
	if !approx(tr.AIDefectDensity, 2.5) { // (1.0 + 4.0) / 2
		t.Errorf("AIDefectDensity = %v, want 2.5", tr.AIDefectDensity)
	}
	if !approx(tr.HumanDefectDensity, 0.5) {
		t.Errorf("HumanDefectDensity = %v, want 0.5", tr.HumanDefectDensity)
	}
	if tr.FirstRun != "2026-01-01T12:00:00Z" || tr.LastRun != "2026-01-03T12:00:00Z" {
		t.Errorf("run window = %s..%s", tr.FirstRun, tr.LastRun)
	}
}

func TestComputeTrendsNoCoverageMeasured(t *testing.T) {
	recs := []ledger.Record{
		{Timestamp: ts(1), TestCoverage: -1, AIGenerated: false},
		{Timestamp: ts(2), TestCoverage: -1, AIGenerated: false},
	}
	tr := ComputeTrends(recs)
	if tr.CoverageRuns != 0 {
		t.Errorf("CoverageRuns = %d, want 0", tr.CoverageRuns)
	}
	if tr.FirstCoverage != -1 || tr.LatestCoverage != -1 || tr.CoverageDelta != 0 {
		t.Errorf("coverage should stay sentinel: %v/%v/%v", tr.FirstCoverage, tr.LatestCoverage, tr.CoverageDelta)
	}
	// All human — AI density sentinel stays -1, human computed.
	if tr.AIDefectDensity != -1 {
		t.Errorf("AIDefectDensity = %v, want -1 (no AI runs)", tr.AIDefectDensity)
	}
}

func TestLoadTrendsMissingFile(t *testing.T) {
	tr, err := LoadTrends("/no/such/ledger.jsonl")
	if err != nil {
		t.Fatalf("LoadTrends errored on missing file: %v", err)
	}
	if tr.Runs != 0 {
		t.Errorf("Runs = %d, want 0", tr.Runs)
	}
}
