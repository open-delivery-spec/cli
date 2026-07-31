package coverage

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// ─── Go coverage.out ─────────────────────────────────────────────

const goCoverage = `mode: set
github.com/x/a.go:1.1,3.2 2 1
github.com/x/a.go:5.1,7.2 3 0
`

func TestParseGo(t *testing.T) {
	path := writeTemp(t, "coverage.out", goCoverage)
	cov, err := parseGo(path)
	if err != nil {
		t.Fatalf("parseGo: %v", err)
	}
	// 2 of 5 statements covered.
	if !approx(cov, 0.4) {
		t.Errorf("coverage = %v, want 0.4", cov)
	}
}

func TestParseGo_NoStatements(t *testing.T) {
	path := writeTemp(t, "coverage.out", "mode: set\n")
	if _, err := parseGo(path); err == nil {
		t.Error("expected error for coverage file with no statements")
	}
}

// ─── LCOV ────────────────────────────────────────────────────────

const lcovCoverage = `TN:
SF:a.go
LF:10
LH:7
end_of_record
SF:b.go
LF:10
LH:3
end_of_record
`

func TestParseLCOV(t *testing.T) {
	path := writeTemp(t, "lcov.info", lcovCoverage)
	cov, err := parseLCOV(path)
	if err != nil {
		t.Fatalf("parseLCOV: %v", err)
	}
	// 10 hit of 20 found across both files.
	if !approx(cov, 0.5) {
		t.Errorf("coverage = %v, want 0.5", cov)
	}
}

func TestParseLCOV_NoLines(t *testing.T) {
	path := writeTemp(t, "lcov.info", "TN:\nSF:a.go\nend_of_record\n")
	if _, err := parseLCOV(path); err == nil {
		t.Error("expected error for LCOV with no LF lines")
	}
}

// ─── Cobertura ───────────────────────────────────────────────────

func TestParseCobertura(t *testing.T) {
	path := writeTemp(t, "coverage.xml", `<?xml version="1.0"?><coverage line-rate="0.85" branch-rate="0.5"></coverage>`)
	cov, err := parseCobertura(path)
	if err != nil {
		t.Fatalf("parseCobertura: %v", err)
	}
	if !approx(cov, 0.85) {
		t.Errorf("coverage = %v, want 0.85", cov)
	}
}

func TestParseCobertura_InvalidRate(t *testing.T) {
	path := writeTemp(t, "coverage.xml", `<coverage line-rate="notanumber"></coverage>`)
	if _, err := parseCobertura(path); err == nil {
		t.Error("expected error for non-numeric line-rate")
	}
}

// ─── NYC / Istanbul ──────────────────────────────────────────────

func TestParseNYC(t *testing.T) {
	path := writeTemp(t, "coverage-summary.json", `{"total":{"lines":{"total":200,"covered":150,"pct":75}}}`)
	cov, err := parseNYC(path)
	if err != nil {
		t.Fatalf("parseNYC: %v", err)
	}
	// 150/200 from total/covered (not the pct field).
	if !approx(cov, 0.75) {
		t.Errorf("coverage = %v, want 0.75", cov)
	}
}

func TestParseNYC_PctFallback(t *testing.T) {
	// When total is 0, parser falls back to the pct field.
	path := writeTemp(t, "coverage-summary.json", `{"total":{"lines":{"total":0,"covered":0,"pct":42}}}`)
	cov, err := parseNYC(path)
	if err != nil {
		t.Fatalf("parseNYC: %v", err)
	}
	if !approx(cov, 0.42) {
		t.Errorf("coverage = %v, want 0.42", cov)
	}
}

func TestParseNYC_NoData(t *testing.T) {
	path := writeTemp(t, "coverage-summary.json", `{"total":{"lines":{"total":0,"covered":0,"pct":0}}}`)
	if _, err := parseNYC(path); err == nil {
		t.Error("expected error when NYC summary has no line data")
	}
}

// ─── Parse auto-detection ────────────────────────────────────────

func TestParse_AutoDetect(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content string
		want    Source
		wantCov float64
	}{
		{"go", "coverage.out", goCoverage, SourceGo, 0.4},
		{"lcov", "lcov.info", lcovCoverage, SourceLCOV, 0.5},
		{"cobertura", "coverage.xml", `<coverage line-rate="0.85"></coverage>`, SourceCobertura, 0.85},
		{"nyc", "coverage-summary.json", `{"total":{"lines":{"total":4,"covered":3,"pct":75}}}`, SourceNYC, 0.75},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeTemp(t, c.file, c.content)
			res := Parse(path)
			if res.Source != c.want {
				t.Errorf("Source = %q, want %q", res.Source, c.want)
			}
			if !approx(res.Coverage, c.wantCov) {
				t.Errorf("Coverage = %v, want %v", res.Coverage, c.wantCov)
			}
		})
	}
}

func TestParse_UnknownFormat(t *testing.T) {
	path := writeTemp(t, "random.txt", "just some plain text\n")
	res := Parse(path)
	if res.Coverage != NotMeasured {
		t.Errorf("Coverage = %v, want NotMeasured", res.Coverage)
	}
	if res.Source != SourceUnknown {
		t.Errorf("Source = %q, want unknown", res.Source)
	}
}

func TestParse_MissingFile(t *testing.T) {
	res := Parse(filepath.Join(t.TempDir(), "nope.out"))
	if res.Coverage != NotMeasured {
		t.Errorf("Coverage = %v, want NotMeasured", res.Coverage)
	}
}

// ─── Detect ──────────────────────────────────────────────────────

func TestDetect_FindsGoCoverage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "coverage.out"), []byte(goCoverage), 0o644); err != nil {
		t.Fatal(err)
	}
	res := Detect(dir)
	if res.Source != SourceGo {
		t.Errorf("Source = %q, want go", res.Source)
	}
	if !approx(res.Coverage, 0.4) {
		t.Errorf("Coverage = %v, want 0.4", res.Coverage)
	}
}

func TestDetect_PriorityGoBeatsLCOV(t *testing.T) {
	// coverage.out is checked before lcov.info; when both exist, Go wins.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "coverage.out"), []byte(goCoverage), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lcov.info"), []byte(lcovCoverage), 0o644); err != nil {
		t.Fatal(err)
	}
	res := Detect(dir)
	if res.Source != SourceGo {
		t.Errorf("Source = %q, want go (priority order)", res.Source)
	}
}

func TestDetect_NotMeasuredWhenEmpty(t *testing.T) {
	res := Detect(t.TempDir())
	if res.Coverage != NotMeasured {
		t.Errorf("Coverage = %v, want NotMeasured", res.Coverage)
	}
	if res.Source != SourceUnknown {
		t.Errorf("Source = %q, want unknown", res.Source)
	}
}

func TestDetect_SubdirLCOV(t *testing.T) {
	// coverage/lcov.info is one of the search locations.
	dir := t.TempDir()
	sub := filepath.Join(dir, "coverage")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "lcov.info"), []byte(lcovCoverage), 0o644); err != nil {
		t.Fatal(err)
	}
	res := Detect(dir)
	if res.Source != SourceLCOV {
		t.Errorf("Source = %q, want lcov", res.Source)
	}
}

// ─── Per-line coverage (patch coverage) ──────────────────────────

func TestParseGoLines(t *testing.T) {
	// a.go:1-3 covered (count 1), a.go:5-7 not covered (count 0).
	path := writeTemp(t, "coverage.out", goCoverage)
	hits, err := parseGoLines(path)
	if err != nil {
		t.Fatalf("parseGoLines: %v", err)
	}
	m := hits["github.com/x/a.go"]
	if m == nil {
		t.Fatalf("no hits for a.go: %v", hits)
	}
	for _, ln := range []int{1, 2, 3} {
		if m[ln] <= 0 {
			t.Errorf("line %d should be covered", ln)
		}
	}
	for _, ln := range []int{5, 6, 7} {
		if hc, ok := m[ln]; !ok || hc != 0 {
			t.Errorf("line %d should be tracked-but-uncovered (hits=0), got %d ok=%v", ln, hc, ok)
		}
	}
	if _, ok := m[4]; ok {
		t.Error("line 4 is between blocks and must not be tracked")
	}
}

func TestParseLCOVLines(t *testing.T) {
	lcov := "SF:src/a.js\nDA:1,3\nDA:2,0\nDA:5,1\nend_of_record\n"
	path := writeTemp(t, "lcov.info", lcov)
	hits, err := parseLCOVLines(path)
	if err != nil {
		t.Fatalf("parseLCOVLines: %v", err)
	}
	m := hits["src/a.js"]
	if m[1] != 3 || m[5] != 1 {
		t.Errorf("covered lines wrong: %v", m)
	}
	if hc, ok := m[2]; !ok || hc != 0 {
		t.Errorf("line 2 should be tracked-uncovered, got %d ok=%v", hc, ok)
	}
}

func TestParseCoberturaLines(t *testing.T) {
	xmlDoc := `<coverage><packages><package><classes>
<class filename="src/a.py"><lines>
<line number="1" hits="2"/><line number="2" hits="0"/>
</lines></class></classes></package></packages></coverage>`
	path := writeTemp(t, "coverage.xml", xmlDoc)
	hits, err := parseCoberturaLines(path)
	if err != nil {
		t.Fatalf("parseCoberturaLines: %v", err)
	}
	m := hits["src/a.py"]
	if m[1] != 2 {
		t.Errorf("line 1 hits = %d, want 2", m[1])
	}
	if hc, ok := m[2]; !ok || hc != 0 {
		t.Errorf("line 2 should be tracked-uncovered, got %d ok=%v", hc, ok)
	}
}

func TestPatchCoverage_suffixMatchAndTracking(t *testing.T) {
	// Coverage keys are import-qualified; diff paths are repo-relative.
	hits := LineHits{
		"github.com/org/repo/internal/svc.go": {10: 1, 11: 0, 12: 1},
	}
	added := map[string][]int{
		// line 10 covered, 11 tracked-uncovered, 13 not tracked (excluded)
		"internal/svc.go": {10, 11, 13},
	}
	covered, total := PatchCoverage(added, hits)
	if covered != 1 || total != 2 {
		t.Errorf("covered/total = %d/%d, want 1/2 (line 13 untracked → excluded)", covered, total)
	}
}

func TestPatchCoverage_unmatchedFileContributesNothing(t *testing.T) {
	hits := LineHits{"github.com/org/repo/a.go": {1: 1}}
	added := map[string][]int{"totally/different.go": {1, 2, 3}}
	covered, total := PatchCoverage(added, hits)
	if covered != 0 || total != 0 {
		t.Errorf("unmatched file must contribute nothing, got %d/%d", covered, total)
	}
}

func TestDetectLines_findsGo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "coverage.out"), []byte(goCoverage), 0o644); err != nil {
		t.Fatal(err)
	}
	hits, src, ok := DetectLines(dir)
	if !ok || src != SourceGo || len(hits) == 0 {
		t.Fatalf("DetectLines = ok=%v src=%v n=%d", ok, src, len(hits))
	}
}

func TestDetectLines_noneFound(t *testing.T) {
	if _, _, ok := DetectLines(t.TempDir()); ok {
		t.Error("DetectLines should report ok=false when no report exists")
	}
}
