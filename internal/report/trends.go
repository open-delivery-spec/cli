package report

import (
	"fmt"

	"github.com/open-delivery-spec/cli/internal/ledger"
)

// Trends summarizes quality/debt signals across ledger records — the history
// git alone cannot reconstruct. It is attached to a Report only when a ledger
// is supplied, so the default git-attribution report is unaffected.
type Trends struct {
	Runs         int     `json:"runs"`
	FirstRun     string  `json:"first_run,omitempty"` // RFC3339 of earliest record
	LastRun      string  `json:"last_run,omitempty"`  // RFC3339 of latest record
	NetDebtDelta float64 `json:"net_debt_delta"`      // sum of technical_debt_delta
	AvgDebtDelta float64 `json:"avg_debt_delta"`

	// Coverage trend, over runs that actually measured coverage (>= 0).
	CoverageRuns   int     `json:"coverage_runs"`
	FirstCoverage  float64 `json:"first_coverage"`  // -1 when none measured
	LatestCoverage float64 `json:"latest_coverage"` // -1 when none measured
	CoverageDelta  float64 `json:"coverage_delta"`  // latest - first
	HighRiskRuns   int     `json:"high_risk_runs"`  // delta > blockThreshold
	HighRiskRate   float64 `json:"high_risk_rate"`  // 0..1

	// AI vs human defect density — the headline governance question, answered
	// from real per-run data rather than assumption.
	AIRuns             int     `json:"ai_runs"`
	HumanRuns          int     `json:"human_runs"`
	AIDefectDensity    float64 `json:"ai_defect_density"`    // avg over AI runs, -1 if none
	HumanDefectDensity float64 `json:"human_defect_density"` // avg over human runs, -1 if none

	Summary string `json:"summary"`
}

// blockThreshold mirrors the scorer's "block" cutoff: a run whose technical debt
// delta exceeds this would be blocked by the default gate.
const blockThreshold = 5.0

// ComputeTrends aggregates ledger records in file (chronological) order. Records
// are assumed appended over time, so the first element is the earliest run.
func ComputeTrends(records []ledger.Record) Trends {
	t := Trends{FirstCoverage: -1, LatestCoverage: -1, AIDefectDensity: -1, HumanDefectDensity: -1}
	if len(records) == 0 {
		t.Summary = "No ledger history yet"
		return t
	}

	t.Runs = len(records)
	t.FirstRun = records[0].Timestamp.UTC().Format("2006-01-02T15:04:05Z")
	t.LastRun = records[len(records)-1].Timestamp.UTC().Format("2006-01-02T15:04:05Z")

	var aiDefectSum, humanDefectSum float64
	for _, r := range records {
		t.NetDebtDelta += r.TechnicalDebtDelta
		if r.TechnicalDebtDelta > blockThreshold {
			t.HighRiskRuns++
		}
		if r.TestCoverage >= 0 {
			if t.CoverageRuns == 0 {
				t.FirstCoverage = r.TestCoverage
			}
			t.LatestCoverage = r.TestCoverage
			t.CoverageRuns++
		}
		if r.AIGenerated {
			t.AIRuns++
			aiDefectSum += r.DefectDensity
		} else {
			t.HumanRuns++
			humanDefectSum += r.DefectDensity
		}
	}

	t.AvgDebtDelta = t.NetDebtDelta / float64(t.Runs)
	t.HighRiskRate = float64(t.HighRiskRuns) / float64(t.Runs)
	if t.CoverageRuns > 0 {
		t.CoverageDelta = t.LatestCoverage - t.FirstCoverage
	}
	if t.AIRuns > 0 {
		t.AIDefectDensity = aiDefectSum / float64(t.AIRuns)
	}
	if t.HumanRuns > 0 {
		t.HumanDefectDensity = humanDefectSum / float64(t.HumanRuns)
	}
	t.Summary = summarizeTrends(t)
	return t
}

func summarizeTrends(t Trends) string {
	debtDir := "flat"
	switch {
	case t.NetDebtDelta > 0.5:
		debtDir = "rising"
	case t.NetDebtDelta < -0.5:
		debtDir = "falling"
	}
	s := fmt.Sprintf("%d run(s): net tech-debt %s (%+.1f), %d high-risk (%.0f%%)",
		t.Runs, debtDir, t.NetDebtDelta, t.HighRiskRuns, t.HighRiskRate*100)
	if t.AIRuns > 0 && t.HumanRuns > 0 {
		s += fmt.Sprintf("; defect density AI %.1f vs human %.1f /KLOC",
			t.AIDefectDensity, t.HumanDefectDensity)
	}
	return s
}

// LoadTrends reads a ledger file and computes its trends. A missing file yields
// zero-value trends with no error, so callers can pass an optional path.
func LoadTrends(path string) (Trends, error) {
	records, err := ledger.Load(path)
	if err != nil {
		return Trends{}, err
	}
	return ComputeTrends(records), nil
}
