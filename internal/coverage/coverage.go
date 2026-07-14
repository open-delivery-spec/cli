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
