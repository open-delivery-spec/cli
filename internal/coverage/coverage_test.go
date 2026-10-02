package coverage

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
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

// ─── Shared fixtures ─────────────────────────────────────────────

const (
	nycCoverage       = `{"total":{"lines":{"total":4,"covered":3,"pct":75}}}`
	coberturaCoverage = `<coverage line-rate="0.85"></coverage>`
)

// writeReport creates rel (which may include subdirectories) under dir.
func writeReport(t *testing.T, dir, rel, content string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", rel, err)
	}
	return path
}

// ─── Unreadable files ────────────────────────────────────────────

// TestParsers_MissingFile guards the open/read failure of every format parser:
// a path that does not exist is reported as an error that still identifies the
// cause, never as a coverage value.
func TestParsers_MissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")

	scalar := map[string]func(string) (float64, error){
		"parseGo": parseGo, "parseLCOV": parseLCOV, "parseCobertura": parseCobertura, "parseNYC": parseNYC,
	}
	for name, parse := range scalar {
		t.Run(name, func(t *testing.T) {
			cov, err := parse(missing)
			if !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s(missing) = (%v, %v), want an os.ErrNotExist error", name, cov, err)
			}
		})
	}

	perLine := map[string]func(string) (LineHits, error){
		"parseGoLines": parseGoLines, "parseLCOVLines": parseLCOVLines, "parseCoberturaLines": parseCoberturaLines,
	}
	for name, parse := range perLine {
		t.Run(name, func(t *testing.T) {
			hits, err := parse(missing)
			if !errors.Is(err, os.ErrNotExist) || hits != nil {
				t.Errorf("%s(missing) = (%v, %v), want (nil, an os.ErrNotExist error)", name, hits, err)
			}
		})
	}
}

// ─── Go coverage.out: malformed input ────────────────────────────

// TestParseGo_SkipsMalformedLines guards tolerance of a damaged report: records
// with the wrong number of fields or a non-numeric statement or hit count are
// skipped, and only the well-formed ones are counted.
func TestParseGo_SkipsMalformedLines(t *testing.T) {
	content := "mode: set\n" +
		"github.com/x/a.go:1.1,3.2 2 1\n" + // valid, covered: 2 statements
		"\n" +
		"two fields\n" +
		"github.com/x/a.go:4.1,5.2 two 1\n" + // statement count is not a number
		"github.com/x/a.go:6.1,7.2 3 many\n" + // hit count is not a number
		"github.com/x/a.go:10.1,11.2 5 1 trailing\n" + // an extra field
		"github.com/x/a.go:8.1,9.2 2 0\n" // valid, not covered: 2 statements
	cov, err := parseGo(writeTemp(t, "coverage.out", content))
	if err != nil {
		t.Fatalf("parseGo: %v", err)
	}
	if !approx(cov, 0.5) {
		t.Errorf("coverage = %v, want 0.5 (2 of the 4 well-formed statements)", cov)
	}
}

// TestParseGo_NothingUsable guards the "no data" error: a report with no
// well-formed record is an error, so callers fall back to NotMeasured instead
// of treating the file as 0% coverage.
func TestParseGo_NothingUsable(t *testing.T) {
	cases := map[string]string{
		"empty file":         "",
		"header only":        "mode: atomic\n",
		"unrelated text":     "this is not a coverage report\n",
		"non-numeric counts": "mode: set\ngithub.com/x/a.go:1.1,3.2 many few\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if cov, err := parseGo(writeTemp(t, "coverage.out", content)); err == nil {
				t.Errorf("parseGo = %v, want an error", cov)
			}
		})
	}
}

// ─── LCOV: malformed input ───────────────────────────────────────

// TestParseLCOV_Counters guards counter handling: surrounding whitespace is
// tolerated, an unreadable LH counts as no hits, and a file whose LF counters
// are all unreadable has no lines found, which is an error.
func TestParseLCOV_Counters(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    float64
		wantErr bool
	}{
		{"indented counters", "  LF:4\n\tLH:3  \n", 0.75, false},
		{"unreadable hit counter is zero hits", "SF:a.go\nLF:10\nLH:many\nend_of_record\n", 0, false},
		{"unreadable found counter leaves nothing found", "SF:a.go\nLF:ten\nLH:5\nend_of_record\n", 0, true},
		{"empty file", "", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cov, err := parseLCOV(writeTemp(t, "lcov.info", c.content))
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseLCOV = %v, want an error", cov)
				}
				return
			}
			if err != nil || !approx(cov, c.want) {
				t.Errorf("parseLCOV = (%v, %v), want (%v, nil)", cov, err, c.want)
			}
		})
	}
}

// ─── Cobertura: malformed input ──────────────────────────────────

// TestParseCobertura_Malformed guards the XML failure modes: a truncated
// document, a document with a different root element, and a root without a
// line-rate attribute are all errors rather than a rate of 0.
func TestParseCobertura_Malformed(t *testing.T) {
	cases := map[string]string{
		"empty file":         "",
		"truncated document": `<coverage line-rate="0.5"`,
		"different root":     `<report line-rate="0.5"></report>`,
		"missing line-rate":  `<coverage branch-rate="0.5"></coverage>`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if cov, err := parseCobertura(writeTemp(t, "coverage.xml", content)); err == nil {
				t.Errorf("parseCobertura = %v, want an error", cov)
			}
		})
	}
}

// ─── NYC: malformed input ────────────────────────────────────────

// TestParseNYC_Malformed guards the JSON failure modes: broken JSON, a total of
// the wrong type, an Istanbul "Unknown" percentage, an empty object and a
// non-positive percentage with no line totals are all errors.
func TestParseNYC_Malformed(t *testing.T) {
	cases := map[string]string{
		"broken json":                `{"total":`,
		"total is not an object":     `{"total":"lots"}`,
		"percentage is a string":     `{"total":{"lines":{"total":0,"covered":0,"pct":"Unknown"}}}`,
		"empty object":               `{}`,
		"negative percentage":        `{"total":{"lines":{"total":0,"covered":0,"pct":-5}}}`,
		"counts without a total key": `{"total":{"lines":{"covered":3}}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if cov, err := parseNYC(writeTemp(t, "coverage-summary.json", content)); err == nil {
				t.Errorf("parseNYC = %v, want an error", cov)
			}
		})
	}
}

// ─── Parse: recognized but unparseable ───────────────────────────

// TestParse_RecognizedButUnparseable guards the format sniffing: a file that
// looks like a supported report but fails its parser is "not measured" with an
// unknown source, and still names the file so the caller can say what was tried.
func TestParse_RecognizedButUnparseable(t *testing.T) {
	cases := map[string]struct{ file, content string }{
		"go header without records":  {"coverage.out", "mode: set\n"},
		"lcov markers without LF":    {"lcov.info", "SF:a.go\nDA:1,1\nend_of_record\n"},
		"cobertura without a rate":   {"coverage.xml", `<coverage line-rate="n/a"></coverage>`},
		"nyc summary without a line": {"coverage-summary.json", `{"total":{"lines":{"total":0,"covered":0,"pct":0}}}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeTemp(t, c.file, c.content)
			res := Parse(path)
			if res.Coverage != NotMeasured || res.Source != SourceUnknown {
				t.Errorf("Parse = %+v, want NotMeasured with source unknown", res)
			}
			if res.File != path {
				t.Errorf("File = %q, want %q", res.File, path)
			}
		})
	}
}

// ─── Detect: search locations and fall-through ───────────────────

// TestDetect_SearchLocations guards the full search list: every documented
// location is found, parsed with its own format and reported with its path.
func TestDetect_SearchLocations(t *testing.T) {
	cases := []struct {
		rel     string
		content string
		want    Source
		wantCov float64
	}{
		{"coverage.out", goCoverage, SourceGo, 0.4},
		{"cover.out", goCoverage, SourceGo, 0.4},
		{"coverage/coverage-summary.json", nycCoverage, SourceNYC, 0.75},
		{"coverage-summary.json", nycCoverage, SourceNYC, 0.75},
		{"lcov.info", lcovCoverage, SourceLCOV, 0.5},
		{"coverage/lcov.info", lcovCoverage, SourceLCOV, 0.5},
		{"coverage.xml", coberturaCoverage, SourceCobertura, 0.85},
		{"coverage/cobertura-coverage.xml", coberturaCoverage, SourceCobertura, 0.85},
	}
	for _, c := range cases {
		t.Run(c.rel, func(t *testing.T) {
			dir := t.TempDir()
			path := writeReport(t, dir, c.rel, c.content)
			res := Detect(dir)
			if res.Source != c.want || !approx(res.Coverage, c.wantCov) || res.File != path {
				t.Errorf("Detect = %+v, want source %q, coverage %v, file %q", res, c.want, c.wantCov, path)
			}
		})
	}
}

// TestDetect_PriorityOrder guards the order of the search: with a report at
// every location they are tried Go, NYC, LCOV, then Cobertura, and within a
// format in the documented order. Each report covers a different tenth, so the
// winner is identifiable, and removing it promotes the next one.
func TestDetect_PriorityOrder(t *testing.T) {
	goReport := func(k int) string {
		return fmt.Sprintf("mode: set\na.go:1.1,2.2 %d 1\na.go:3.1,4.2 %d 0\n", k, 10-k)
	}
	nycReport := func(k int) string {
		return fmt.Sprintf(`{"total":{"lines":{"total":10,"covered":%d}}}`, k)
	}
	lcovReport := func(k int) string {
		return fmt.Sprintf("SF:a.js\nLF:10\nLH:%d\nend_of_record\n", k)
	}
	coberturaReport := func(k int) string {
		return fmt.Sprintf(`<coverage line-rate="0.%d"></coverage>`, k)
	}
	order := []struct {
		rel    string
		render func(int) string
		source Source
	}{
		{"coverage.out", goReport, SourceGo},
		{"cover.out", goReport, SourceGo},
		{"coverage/coverage-summary.json", nycReport, SourceNYC},
		{"coverage-summary.json", nycReport, SourceNYC},
		{"lcov.info", lcovReport, SourceLCOV},
		{"coverage/lcov.info", lcovReport, SourceLCOV},
		{"coverage.xml", coberturaReport, SourceCobertura},
		{"coverage/cobertura-coverage.xml", coberturaReport, SourceCobertura},
	}

	dir := t.TempDir()
	paths := make([]string, len(order))
	for i, o := range order {
		paths[i] = writeReport(t, dir, o.rel, o.render(i+1))
	}
	for i, o := range order {
		want := float64(i+1) / 10
		res := Detect(dir)
		if res.File != paths[i] || res.Source != o.source || !approx(res.Coverage, want) {
			t.Fatalf("with %d report(s) left, Detect = %+v, want %s coverage %v from %s", len(order)-i, res, o.source, want, o.rel)
		}
		if err := os.Remove(paths[i]); err != nil {
			t.Fatal(err)
		}
	}
	if res := Detect(dir); res.Coverage != NotMeasured {
		t.Errorf("Detect with no reports left = %+v, want NotMeasured", res)
	}
}

// TestDetect_ZeroCoverageIsMeasured guards the boundary of the "not measured"
// sentinel: a report that says nothing was covered is a real 0%, found and
// reported as such, not skipped as if it were missing.
func TestDetect_ZeroCoverageIsMeasured(t *testing.T) {
	dir := t.TempDir()
	path := writeReport(t, dir, "coverage.out", "mode: set\ngithub.com/x/a.go:1.1,3.2 4 0\n")
	res := Detect(dir)
	if res.Source != SourceGo || res.Coverage != 0 || res.File != path {
		t.Errorf("Detect = %+v, want a measured 0%% go report from %s", res, path)
	}
	if res.Coverage == NotMeasured {
		t.Error("0% coverage must not be confused with NotMeasured")
	}
}

// TestDetect_SkipsUnusableReports guards the fall-through: a candidate that
// exists but cannot yield a coverage fraction (unparseable, a negative rate, or
// not even a file) does not end the search, and a directory with nothing usable
// is "not measured".
func TestDetect_SkipsUnusableReports(t *testing.T) {
	t.Run("empty go report falls through to lcov", func(t *testing.T) {
		dir := t.TempDir()
		writeReport(t, dir, "coverage.out", "mode: set\n")
		lcov := writeReport(t, dir, "lcov.info", lcovCoverage)
		if res := Detect(dir); res.Source != SourceLCOV || res.File != lcov {
			t.Errorf("Detect = %+v, want the lcov report %s", res, lcov)
		}
	})

	t.Run("negative cobertura rate falls through to the next location", func(t *testing.T) {
		dir := t.TempDir()
		writeReport(t, dir, "coverage.xml", `<coverage line-rate="-1"></coverage>`)
		next := writeReport(t, dir, "coverage/cobertura-coverage.xml", coberturaCoverage)
		res := Detect(dir)
		if res.Source != SourceCobertura || res.File != next || !approx(res.Coverage, 0.85) {
			t.Errorf("Detect = %+v, want 0.85 from %s", res, next)
		}
	})

	t.Run("a directory named like a report is skipped", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "coverage.out"), 0o755); err != nil {
			t.Fatal(err)
		}
		lcov := writeReport(t, dir, "lcov.info", lcovCoverage)
		if res := Detect(dir); res.Source != SourceLCOV || res.File != lcov {
			t.Errorf("Detect = %+v, want the lcov report %s", res, lcov)
		}
	})

	t.Run("nothing usable is not measured", func(t *testing.T) {
		dir := t.TempDir()
		writeReport(t, dir, "coverage.out", "garbage\n")
		writeReport(t, dir, "coverage.xml", "<coverage>")
		res := Detect(dir)
		if res.Coverage != NotMeasured || res.Source != SourceUnknown || res.File != "" {
			t.Errorf("Detect = %+v, want NotMeasured, unknown, and no file", res)
		}
	})
}

// ─── Per-line parsing: Go coverage.out ───────────────────────────

// TestParseGoRange guards the range parser: the start and end lines are read
// from "line.col,line.col", columns are optional, an end before the start
// collapses to the start line, and anything else is rejected.
func TestParseGoRange(t *testing.T) {
	cases := []struct {
		in         string
		start, end int
		ok         bool
	}{
		{"1.1,3.2", 1, 3, true},
		{"10.5,10.9", 10, 10, true},
		{"7,9", 7, 9, true},
		{"5.1,3.2", 5, 5, true},
		{"", 0, 0, false},
		{"1.1", 0, 0, false},
		{"a.1,3.2", 0, 0, false},
		{"1.1,b.2", 0, 0, false},
		{".1,3.2", 0, 0, false},
	}
	for _, c := range cases {
		start, end, ok := parseGoRange(c.in)
		if start != c.start || end != c.end || ok != c.ok {
			t.Errorf("parseGoRange(%q) = (%d, %d, %v), want (%d, %d, %v)", c.in, start, end, ok, c.start, c.end, c.ok)
		}
	}
}

// TestParseGoLines_SkipsMalformedRecords guards tolerance of a damaged report:
// each kind of broken record is skipped on its own, so a file made only of
// them has no per-line data (an error), while valid records beside them
// are still read.
func TestParseGoLines_SkipsMalformedRecords(t *testing.T) {
	broken := map[string]string{
		"too few fields":      "a.go:1.1,2.2 1",
		"too many fields":     "a.go:1.1,2.2 1 1 extra",
		"hit count not a int": "a.go:1.1,2.2 1 many",
		"no file separator":   "a.go 1 1",
		"range without comma": "a.go:oops 1 1",
		"start not a number":  "a.go:x.1,3.2 1 1",
		"end not a number":    "a.go:1.1,y.2 1 1",
	}
	for name, record := range broken {
		t.Run(name, func(t *testing.T) {
			path := writeTemp(t, "coverage.out", "mode: set\n"+record+"\n")
			if hits, err := parseGoLines(path); err == nil {
				t.Errorf("parseGoLines = %v, want a no-data error", hits)
			}
		})
	}

	t.Run("valid records beside broken ones survive", func(t *testing.T) {
		content := "mode: set\n"
		for _, record := range broken {
			content += record + "\n"
		}
		content += "good.go:3.1,4.2 1 2\n"
		hits, err := parseGoLines(writeTemp(t, "coverage.out", content))
		if err != nil {
			t.Fatalf("parseGoLines: %v", err)
		}
		if want := (LineHits{"good.go": {3: 2, 4: 2}}); !reflect.DeepEqual(hits, want) {
			t.Errorf("hits = %v, want %v", hits, want)
		}
	})
}

// TestParseGoLines_BlockShapes guards how block ranges map to lines: an end
// before the start marks only the start line, columns are optional, a Windows
// drive letter in the file name does not confuse the file/range split, and
// overlapping blocks keep the highest hit count whatever their order.
func TestParseGoLines_BlockShapes(t *testing.T) {
	content := "mode: count\n" +
		"a.go:5.1,3.2 1 1\n" + // end before start: line 5 only
		"a.go:7,9 1 0\n" + // no columns: lines 7-9
		`C:\src\b.go:2.1,2.9 1 4` + "\n" +
		"c.go:1.1,3.2 1 0\n" + // overlap, uncovered first ...
		"c.go:2.1,2.5 1 6\n" + // ... then covered
		"c.go:3.1,3.5 1 0\n"
	hits, err := parseGoLines(writeTemp(t, "coverage.out", content))
	if err != nil {
		t.Fatalf("parseGoLines: %v", err)
	}
	want := LineHits{
		"a.go":        {5: 1, 7: 0, 8: 0, 9: 0},
		`C:\src\b.go`: {2: 4},
		"c.go":        {1: 0, 2: 6, 3: 0},
	}
	if !reflect.DeepEqual(hits, want) {
		t.Errorf("hits = %v, want %v", hits, want)
	}
}

// ─── Per-line parsing: LCOV ──────────────────────────────────────

// TestParseLCOVLines_Records guards record handling: DA lines belong to the
// SF record that precedes them (before the first SF or after end_of_record
// they are ignored), whitespace is trimmed, malformed DA lines are skipped,
// duplicate line entries keep the highest count, and each file is tracked on
// its own.
func TestParseLCOVLines_Records(t *testing.T) {
	content := "DA:1,5\n" + // before any SF
		"SF:src/a.js\n" +
		"DA:1,0\n" +
		"DA:1,3\n" + // same line again: highest count wins
		"DA:2,5\n" +
		"DA:2,0\n" +
		"DA:3\n" + // no hit count
		"DA:x,1\n" + // line is not a number
		"DA:4,y\n" + // hit count is not a number
		"DA: 6 , 2 \n" + // whitespace around the fields
		"end_of_record\n" +
		"DA:9,9\n" + // after the record closed
		"SF:src/b.js\n" +
		"DA:1,1\n" +
		"end_of_record\n"
	hits, err := parseLCOVLines(writeTemp(t, "lcov.info", content))
	if err != nil {
		t.Fatalf("parseLCOVLines: %v", err)
	}
	want := LineHits{"src/a.js": {1: 3, 2: 5, 6: 2}, "src/b.js": {1: 1}}
	if !reflect.DeepEqual(hits, want) {
		t.Errorf("hits = %v, want %v", hits, want)
	}
}

// TestParseLCOVLines_NoLineData guards the "no data" error: a report whose
// records carry no usable DA line (aggregate counters only, or none at all) is an
// error rather than an empty map.
func TestParseLCOVLines_NoLineData(t *testing.T) {
	cases := map[string]string{
		"empty file":      "",
		"counters only":   "SF:a.js\nLF:10\nLH:7\nend_of_record\n",
		"DA outside a SF": "DA:1,1\nend_of_record\n",
		"only bad DA":     "SF:a.js\nDA:1\nDA:x,y\nend_of_record\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if hits, err := parseLCOVLines(writeTemp(t, "lcov.info", content)); err == nil {
				t.Errorf("parseLCOVLines = %v, want a no-data error", hits)
			}
		})
	}
}

// ─── Per-line parsing: Cobertura ─────────────────────────────────

// TestParseCoberturaLines_Documents guards class handling: lines from every
// package and class are collected per filename, classes without a filename are
// skipped, and a line listed twice keeps the highest hit count.
func TestParseCoberturaLines_Documents(t *testing.T) {
	doc := `<coverage><packages>
<package><classes>
<class filename="src/a.py"><lines>
<line number="1" hits="0"/><line number="1" hits="4"/><line number="2" hits="0"/>
</lines></class>
<class filename=""><lines><line number="9" hits="9"/></lines></class>
<class><lines><line number="8" hits="8"/></lines></class>
</classes></package>
<package><classes>
<class filename="src/b.py"><lines><line number="5" hits="1"/></lines></class>
</classes></package>
</packages></coverage>`
	hits, err := parseCoberturaLines(writeTemp(t, "coverage.xml", doc))
	if err != nil {
		t.Fatalf("parseCoberturaLines: %v", err)
	}
	want := LineHits{"src/a.py": {1: 4, 2: 0}, "src/b.py": {5: 1}}
	if !reflect.DeepEqual(hits, want) {
		t.Errorf("hits = %v, want %v", hits, want)
	}
}

// TestParseCoberturaLines_Unusable guards the failure modes: malformed XML, and
// well-formed XML with no class lines (a rate-only report, or classes lacking a
// filename), are errors.
func TestParseCoberturaLines_Unusable(t *testing.T) {
	cases := map[string]string{
		"malformed xml":    `<coverage><packages>`,
		"rate only":        `<coverage line-rate="0.85"></coverage>`,
		"no filenames":     `<coverage><packages><package><classes><class><lines><line number="1" hits="1"/></lines></class></classes></package></packages></coverage>`,
		"classes no lines": `<coverage><packages><package><classes><class filename="a.py"></class></classes></package></packages></coverage>`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if hits, err := parseCoberturaLines(writeTemp(t, "coverage.xml", content)); err == nil {
				t.Errorf("parseCoberturaLines = %v, want an error", hits)
			}
		})
	}
}

// ─── DetectLines: search locations and fall-through ──────────────

// TestDetectLines_SearchLocations guards the per-line search list: each
// location is found, parsed with its own format and reported with its source.
func TestDetectLines_SearchLocations(t *testing.T) {
	const coberturaLines = `<coverage><packages><package><classes><class filename="a.py"><lines><line number="1" hits="1"/></lines></class></classes></package></packages></coverage>`
	const lcovLines = "SF:a.js\nDA:1,1\nend_of_record\n"

	cases := []struct {
		rel     string
		content string
		want    Source
		wantKey string
	}{
		{"coverage.out", goCoverage, SourceGo, "github.com/x/a.go"},
		{"cover.out", goCoverage, SourceGo, "github.com/x/a.go"},
		{"lcov.info", lcovLines, SourceLCOV, "a.js"},
		{"coverage/lcov.info", lcovLines, SourceLCOV, "a.js"},
		{"coverage.xml", coberturaLines, SourceCobertura, "a.py"},
		{"coverage/cobertura-coverage.xml", coberturaLines, SourceCobertura, "a.py"},
	}
	for _, c := range cases {
		t.Run(c.rel, func(t *testing.T) {
			dir := t.TempDir()
			writeReport(t, dir, c.rel, c.content)
			hits, src, ok := DetectLines(dir)
			if !ok || src != c.want {
				t.Fatalf("DetectLines = (ok=%v, src=%q), want (true, %q)", ok, src, c.want)
			}
			if _, found := hits[c.wantKey]; !found {
				t.Errorf("hits = %v, want an entry for %q", hits, c.wantKey)
			}
		})
	}
}

// TestDetectLines_PriorityOrder guards the order of the per-line search: Go,
// then LCOV, then Cobertura, and within a format in the documented order. Each
// report names a different file, so the winner is identifiable, and removing it
// promotes the next one.
func TestDetectLines_PriorityOrder(t *testing.T) {
	cobertura := func(file string) string {
		return `<coverage><packages><package><classes><class filename="` + file + `"><lines><line number="1" hits="1"/></lines></class></classes></package></packages></coverage>`
	}
	order := []struct {
		rel     string
		content string
		source  Source
		key     string
	}{
		{"coverage.out", "mode: set\nfirst.go:1.1,1.9 1 1\n", SourceGo, "first.go"},
		{"cover.out", "mode: set\nsecond.go:1.1,1.9 1 1\n", SourceGo, "second.go"},
		{"lcov.info", "SF:third.js\nDA:1,1\nend_of_record\n", SourceLCOV, "third.js"},
		{"coverage/lcov.info", "SF:fourth.js\nDA:1,1\nend_of_record\n", SourceLCOV, "fourth.js"},
		{"coverage.xml", cobertura("fifth.py"), SourceCobertura, "fifth.py"},
		{"coverage/cobertura-coverage.xml", cobertura("sixth.py"), SourceCobertura, "sixth.py"},
	}

	dir := t.TempDir()
	paths := make([]string, len(order))
	for i, o := range order {
		paths[i] = writeReport(t, dir, o.rel, o.content)
	}
	for i, o := range order {
		hits, src, ok := DetectLines(dir)
		if !ok || src != o.source || !reflect.DeepEqual(hits, LineHits{o.key: {1: 1}}) {
			t.Fatalf("with %d report(s) left, DetectLines = (%v, %q, %v), want %s hits for %s", len(order)-i, hits, src, ok, o.source, o.key)
		}
		if err := os.Remove(paths[i]); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, ok := DetectLines(dir); ok {
		t.Error("DetectLines with no reports left should report ok=false")
	}
}

// TestDetectLines_SkipsUnusableReports guards the fall-through and the NYC
// exclusion: an unusable per-line report does not end the search, and NYC's
// aggregate-only summary is never mistaken for per-line data. When nothing is
// found the result is empty with an unknown source.
func TestDetectLines_SkipsUnusableReports(t *testing.T) {
	t.Run("empty go report falls through to lcov", func(t *testing.T) {
		dir := t.TempDir()
		writeReport(t, dir, "coverage.out", "mode: set\n")
		writeReport(t, dir, "lcov.info", "SF:a.js\nDA:1,1\nend_of_record\n")
		hits, src, ok := DetectLines(dir)
		if !ok || src != SourceLCOV || !reflect.DeepEqual(hits, LineHits{"a.js": {1: 1}}) {
			t.Errorf("DetectLines = (%v, %q, %v), want the lcov hits", hits, src, ok)
		}
	})

	t.Run("nyc summary alone is not per-line data", func(t *testing.T) {
		dir := t.TempDir()
		writeReport(t, dir, "coverage-summary.json", nycCoverage)
		writeReport(t, dir, "coverage/coverage-summary.json", nycCoverage)
		hits, src, ok := DetectLines(dir)
		if ok || hits != nil || src != SourceUnknown {
			t.Errorf("DetectLines = (%v, %q, %v), want (nil, unknown, false)", hits, src, ok)
		}
	})
}

// ─── matchFile / PatchCoverage ───────────────────────────────────

// TestMatchFile guards how a diff path finds its coverage entry when only one
// entry can qualify: exact name, a coverage key that is import-qualified or
// absolute, a coverage key shorter than the diff path, and a unique basename
// as the last resort. Suffixes must align on a path separator; a basename
// shared by several entries is ambiguous and matches nothing. Wherever a
// primary rule is under test a same-named distractor is present, so the
// basename fallback cannot stand in for it.
func TestMatchFile(t *testing.T) {
	cases := []struct {
		name    string
		diff    string
		keys    []string
		wantKey string // "" = no match
	}{
		{"exact path", "a/b.go", []string{"a/b.go", "c/b.go"}, "a/b.go"},
		{
			"import-qualified key",
			"internal/svc.go",
			[]string{"github.com/org/repo/internal/svc.go", "github.com/org/repo/legacy/svc.go"},
			"github.com/org/repo/internal/svc.go",
		},
		{
			"absolute key",
			"internal/svc.go",
			[]string{"/home/ci/work/repo/internal/svc.go", "/home/ci/work/repo/legacy/svc.go"},
			"/home/ci/work/repo/internal/svc.go",
		},
		{"key shorter than the diff path", "services/api/handler.go", []string{"api/handler.go", "legacy/handler.go"}, "api/handler.go"},
		{"suffix must align on a separator", "svc.go", []string{"github.com/org/repo/xsvc.go"}, ""},
		{"unique basename as a last resort", "cmd/tool/handler.go", []string{"vendored/pkg/handler.go", "pkg/other.go"}, "vendored/pkg/handler.go"},
		{"ambiguous basename matches nothing", "cmd/tool/handler.go", []string{"a/handler.go", "b/handler.go"}, ""},
		{"no relation at all", "cmd/tool/main.go", []string{"pkg/other.go"}, ""},
		{"no coverage entries", "a.go", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Each key gets its own marker hit count so the matched entry is identifiable.
			hits := LineHits{}
			for i, k := range c.keys {
				hits[k] = map[int]int{1: 100 + i}
			}
			got := matchFile(c.diff, hits)
			if c.wantKey == "" {
				if got != nil {
					t.Errorf("matchFile(%q) = %v, want no match", c.diff, got)
				}
				return
			}
			if want := hits[c.wantKey]; !reflect.DeepEqual(got, want) {
				t.Errorf("matchFile(%q) = %v, want the entry for %q (%v)", c.diff, got, c.wantKey, want)
			}
		})
	}
}

// TestPatchCoverage_AcrossFiles guards the roll-up: covered and tracked lines
// are summed over every matched file, untracked lines and unmatched files add
// nothing, and an empty diff or an empty report gives 0/0 (the caller's cue
// for "not measured").
func TestPatchCoverage_AcrossFiles(t *testing.T) {
	hits := LineHits{
		"github.com/org/repo/a.go": {1: 2, 2: 0, 3: 1},
		"github.com/org/repo/b.go": {10: 0},
	}
	added := map[string][]int{
		"a.go":      {1, 2, 4}, // 1 covered, 2 tracked-uncovered, 4 untracked
		"b.go":      {10},      // tracked-uncovered
		"gone/c.go": {1, 2},    // no coverage entry
	}
	if covered, total := PatchCoverage(added, hits); covered != 1 || total != 3 {
		t.Errorf("covered/total = %d/%d, want 1/3", covered, total)
	}
	if covered, total := PatchCoverage(nil, hits); covered != 0 || total != 0 {
		t.Errorf("empty diff: covered/total = %d/%d, want 0/0", covered, total)
	}
	if covered, total := PatchCoverage(added, LineHits{}); covered != 0 || total != 0 {
		t.Errorf("empty report: covered/total = %d/%d, want 0/0", covered, total)
	}
}
