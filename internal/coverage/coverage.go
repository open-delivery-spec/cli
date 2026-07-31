// Package coverage detects and parses test coverage reports.
// It supports Go coverage.out, LCOV lcov.info, Cobertura coverage.xml,
// and NYC coverage-summary.json. When no coverage file is found it returns
// the sentinel value −1.0 ("not measured") so callers can skip the
// coverage penalty rather than treating unmeasured coverage as 0%.
package coverage

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// NotMeasured is returned when no coverage file can be found or parsed.
// Callers (scorer, policy input builder) MUST check for this value and
// skip the coverage penalty rather than treating it as 0%.
const NotMeasured = -1.0

// Source identifies the format that was successfully parsed.
type Source string

const (
	SourceGo        Source = "go"
	SourceLCOV      Source = "lcov"
	SourceCobertura Source = "cobertura"
	SourceNYC       Source = "nyc"
	SourceUnknown   Source = "unknown"
)

// Result carries the parsed coverage fraction and its provenance.
type Result struct {
	// Coverage is the fraction of lines covered in [0,1], or NotMeasured (−1).
	Coverage float64
	// Source indicates which parser produced the result.
	Source Source
	// File is the path of the coverage file that was parsed.
	File string
}

// Detect searches common locations for a coverage report and returns the
// first one it can parse. It returns NotMeasured when nothing is found.
func Detect(dir string) Result {
	candidates := []struct {
		pattern string
		parse   func(string) (float64, error)
		source  Source
	}{
		// Go coverage
		{"coverage.out", parseGo, SourceGo},
		{"cover.out", parseGo, SourceGo},
		// NYC / Istanbul
		{"coverage/coverage-summary.json", parseNYC, SourceNYC},
		{"coverage-summary.json", parseNYC, SourceNYC},
		// LCOV
		{"lcov.info", parseLCOV, SourceLCOV},
		{"coverage/lcov.info", parseLCOV, SourceLCOV},
		// Cobertura
		{"coverage.xml", parseCobertura, SourceCobertura},
		{"coverage/cobertura-coverage.xml", parseCobertura, SourceCobertura},
	}

	for _, c := range candidates {
		path := filepath.Join(dir, c.pattern)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		cov, err := c.parse(path)
		if err == nil && cov >= 0 {
			return Result{Coverage: cov, Source: c.source, File: path}
		}
	}
	return Result{Coverage: NotMeasured, Source: SourceUnknown}
}

// Parse reads a single coverage file, auto-detecting its format.
// Returns NotMeasured when the file cannot be parsed.
func Parse(path string) Result {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Coverage: NotMeasured, Source: SourceUnknown}
	}
	content := string(data)

	// Go coverage.out starts with "mode:"
	if strings.HasPrefix(strings.TrimSpace(content), "mode:") {
		if cov, err := parseGo(path); err == nil {
			return Result{Coverage: cov, Source: SourceGo, File: path}
		}
	}
	// LCOV starts with "SF:"
	if strings.Contains(content, "SF:") && strings.Contains(content, "end_of_record") {
		if cov, err := parseLCOV(path); err == nil {
			return Result{Coverage: cov, Source: SourceLCOV, File: path}
		}
	}
	// Cobertura XML
	if strings.Contains(content, "<coverage") {
		if cov, err := parseCobertura(path); err == nil {
			return Result{Coverage: cov, Source: SourceCobertura, File: path}
		}
	}
	// NYC JSON
	if strings.Contains(content, `"total"`) {
		if cov, err := parseNYC(path); err == nil {
			return Result{Coverage: cov, Source: SourceNYC, File: path}
		}
	}
	return Result{Coverage: NotMeasured, Source: SourceUnknown, File: path}
}

// ─── Go coverage.out ──────────────────────────────────────────────────────────
//
// Format per line:
//   <file>:<startLine>.<startCol>,<endLine>.<endCol> <numStatements> <count>
// A count > 0 means the block was executed.

func parseGo(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	total := 0
	covered := 0

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		// Split off the count and numStatements
		// pattern: "file:start,end numStatements count"
		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}
		numStmts, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		count, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}
		total += numStmts
		if count > 0 {
			covered += numStmts
		}
	}

	if total == 0 {
		return 0, fmt.Errorf("no statements in Go coverage file")
	}
	return float64(covered) / float64(total), nil
}

// ─── LCOV ────────────────────────────────────────────────────────────────────
//
// Relevant lines:
//   LH:<lines-hit>   (per file record)
//   LF:<lines-found> (per file record)

func parseLCOV(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	totalFound := 0
	totalHit := 0

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "LF:") {
			n, err := strconv.Atoi(strings.TrimPrefix(line, "LF:"))
			if err == nil {
				totalFound += n
			}
		} else if strings.HasPrefix(line, "LH:") {
			n, err := strconv.Atoi(strings.TrimPrefix(line, "LH:"))
			if err == nil {
				totalHit += n
			}
		}
	}

	if totalFound == 0 {
		return 0, fmt.Errorf("no lines found in LCOV file")
	}
	return float64(totalHit) / float64(totalFound), nil
}

// ─── Cobertura XML ───────────────────────────────────────────────────────────
//
// Root element carries line-rate attribute:
//   <coverage line-rate="0.85" ...>

type coberturaXML struct {
	XMLName  xml.Name `xml:"coverage"`
	LineRate string   `xml:"line-rate,attr"`
}

func parseCobertura(path string) (float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var cov coberturaXML
	if err := xml.Unmarshal(data, &cov); err != nil {
		return 0, err
	}
	rate, err := strconv.ParseFloat(cov.LineRate, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid line-rate in Cobertura XML: %w", err)
	}
	return rate, nil
}

// ─── NYC / Istanbul coverage-summary.json ────────────────────────────────────
//
// Minimal relevant structure:
//   {"total":{"lines":{"total":100,"covered":85,"pct":85}}}

type nycSummary struct {
	Total struct {
		Lines struct {
			Total   int     `json:"total"`
			Covered int     `json:"covered"`
			Pct     float64 `json:"pct"`
		} `json:"lines"`
	} `json:"total"`
}

func parseNYC(path string) (float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var s nycSummary
	if err := json.Unmarshal(data, &s); err != nil {
		return 0, err
	}
	if s.Total.Lines.Total == 0 {
		// Fall back to pct field if total is not set
		if s.Total.Lines.Pct > 0 {
			return s.Total.Lines.Pct / 100.0, nil
		}
		return 0, fmt.Errorf("no lines data in NYC coverage summary")
	}
	return float64(s.Total.Lines.Covered) / float64(s.Total.Lines.Total), nil
}

// ─── Per-line coverage (for patch / diff coverage) ───────────────────────────
//
// LineHits maps a coverage-report file path to line number → hit count. A line
// present with hit count 0 is *tracked* (executable, measured) but not covered.
// Lines absent from the map are not executable (blank/comment) and are excluded
// from patch-coverage denominators. Only formats that carry per-line data are
// supported here: Go coverage.out, LCOV, Cobertura. NYC's summary file is
// aggregate-only and is intentionally omitted.
type LineHits map[string]map[int]int

func (h LineHits) mark(file string, line, hits int) {
	m := h[file]
	if m == nil {
		m = map[int]int{}
		h[file] = m
	}
	if hits > m[line] {
		m[line] = hits
	} else if _, ok := m[line]; !ok {
		m[line] = hits
	}
}

// DetectLines finds a per-line-capable coverage report under dir and returns its
// hit map. ok is false when no such report is found or it cannot be parsed.
func DetectLines(dir string) (hits LineHits, source Source, ok bool) {
	candidates := []struct {
		pattern string
		parse   func(string) (LineHits, error)
		source  Source
	}{
		{"coverage.out", parseGoLines, SourceGo},
		{"cover.out", parseGoLines, SourceGo},
		{"lcov.info", parseLCOVLines, SourceLCOV},
		{"coverage/lcov.info", parseLCOVLines, SourceLCOV},
		{"coverage.xml", parseCoberturaLines, SourceCobertura},
		{"coverage/cobertura-coverage.xml", parseCoberturaLines, SourceCobertura},
	}
	for _, c := range candidates {
		path := filepath.Join(dir, c.pattern)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		h, err := c.parse(path)
		if err == nil && len(h) > 0 {
			return h, c.source, true
		}
	}
	return nil, SourceUnknown, false
}

func parseGoLines(path string) (LineHits, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	hits := LineHits{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		// file:startLine.startCol,endLine.endCol numStatements count
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		count, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}
		colon := strings.LastIndex(fields[0], ":")
		if colon < 0 {
			continue
		}
		file := fields[0][:colon]
		rng := fields[0][colon+1:]
		startLine, endLine, ok := parseGoRange(rng)
		if !ok {
			continue
		}
		for ln := startLine; ln <= endLine; ln++ {
			hits.mark(file, ln, count)
		}
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("no per-line data in Go coverage file")
	}
	return hits, nil
}

// parseGoRange parses "startLine.startCol,endLine.endCol" → (startLine, endLine).
func parseGoRange(rng string) (int, int, bool) {
	comma := strings.Index(rng, ",")
	if comma < 0 {
		return 0, 0, false
	}
	start := rng[:comma]
	end := rng[comma+1:]
	sl, ok := atoiBefore(start, '.')
	if !ok {
		return 0, 0, false
	}
	el, ok := atoiBefore(end, '.')
	if !ok {
		return 0, 0, false
	}
	if el < sl {
		el = sl
	}
	return sl, el, true
}

func atoiBefore(s string, sep byte) (int, bool) {
	if i := strings.IndexByte(s, sep); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

func parseLCOVLines(path string) (LineHits, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	hits := LineHits{}
	current := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "SF:"):
			current = strings.TrimPrefix(line, "SF:")
		case strings.HasPrefix(line, "DA:") && current != "":
			parts := strings.SplitN(strings.TrimPrefix(line, "DA:"), ",", 2)
			if len(parts) != 2 {
				continue
			}
			ln, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			hc, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err1 == nil && err2 == nil {
				hits.mark(current, ln, hc)
			}
		case line == "end_of_record":
			current = ""
		}
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("no per-line data in LCOV file")
	}
	return hits, nil
}

type coberturaLinesXML struct {
	Packages struct {
		Package []struct {
			Classes struct {
				Class []struct {
					Filename string `xml:"filename,attr"`
					Lines    struct {
						Line []struct {
							Number int `xml:"number,attr"`
							Hits   int `xml:"hits,attr"`
						} `xml:"line"`
					} `xml:"lines"`
				} `xml:"class"`
			} `xml:"classes"`
		} `xml:"package"`
	} `xml:"packages"`
}

func parseCoberturaLines(path string) (LineHits, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cov coberturaLinesXML
	if err := xml.Unmarshal(data, &cov); err != nil {
		return nil, err
	}
	hits := LineHits{}
	for _, pkg := range cov.Packages.Package {
		for _, cls := range pkg.Classes.Class {
			if cls.Filename == "" {
				continue
			}
			for _, ln := range cls.Lines.Line {
				hits.mark(cls.Filename, ln.Number, ln.Hits)
			}
		}
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("no per-line data in Cobertura file")
	}
	return hits, nil
}

// PatchCoverage computes coverage of a diff's added lines. addedByFile maps a
// repo-relative path to the new-file line numbers it added; hits is the report's
// per-line data (whose paths may be import-qualified or absolute). Files are
// matched by longest path-suffix. Returns (covered, total) where total counts
// added lines that are tracked (executable) in the report — added lines with no
// coverage entry (blank/comment, or a file the report doesn't cover) are
// excluded from both. When total is 0 the caller should treat patch coverage as
// not measured (−1).
func PatchCoverage(addedByFile map[string][]int, hits LineHits) (covered, total int) {
	for diffPath, lines := range addedByFile {
		fileHits := matchFile(diffPath, hits)
		if fileHits == nil {
			continue
		}
		for _, ln := range lines {
			hc, tracked := fileHits[ln]
			if !tracked {
				continue
			}
			total++
			if hc > 0 {
				covered++
			}
		}
	}
	return covered, total
}

// matchFile finds the coverage entry whose path best matches a repo-relative
// diff path: prefer a coverage key that ends with the diff path (longest such
// key wins); fall back to a unique basename match.
func matchFile(diffPath string, hits LineHits) map[int]int {
	best := ""
	for cov := range hits {
		if cov == diffPath || strings.HasSuffix(cov, "/"+diffPath) || strings.HasSuffix(diffPath, "/"+cov) {
			if len(cov) > len(best) {
				best = cov
			}
		}
	}
	if best != "" {
		return hits[best]
	}
	// Fallback: unique basename match.
	base := diffPath
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	var found map[int]int
	matches := 0
	for cov, m := range hits {
		cb := cov
		if i := strings.LastIndex(cb, "/"); i >= 0 {
			cb = cb[i+1:]
		}
		if cb == base {
			found = m
			matches++
		}
	}
	if matches == 1 {
		return found
	}
	return nil
}
