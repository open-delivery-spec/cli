// Package sarif parses SARIF v2.1.0 results into ODS analyzer issues.
// It is deliberately minimal — it only extracts what the ODS policy input
// schema requires (rule, severity, file, line, message) and ignores the
// large portions of SARIF that ODS does not consume.
package sarif

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/open-delivery-spec/cli/internal/analyzer"
)

// sarifDoc is the minimal SARIF v2.1.0 document structure we need.
type sarifDoc struct {
	Version string    `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name  string      `json:"name"`
	Rules []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string        `json:"id"`
	ShortDescription sarifMessage  `json:"shortDescription"`
	Properties       sarifRuleProps `json:"properties"`
}

type sarifRuleProps struct {
	// Semgrep uses "severity" in properties; CodeQL uses "problem.severity"
	Severity        string `json:"severity"`
	ProblemSeverity string `json:"problem.severity"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	RuleIndex int             `json:"ruleIndex"`
	Level     string          `json:"level"` // "error", "warning", "note", "none"
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           sarifRegion   `json:"region"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

// Load reads a SARIF file and converts its results to ODS analyzer issues.
// Returns an empty slice (not an error) when the file contains zero results.
func Load(path string) ([]analyzer.Issue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading SARIF file %s: %w", path, err)
	}

	var doc sarifDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing SARIF file %s: %w", path, err)
	}

	var issues []analyzer.Issue
	for _, run := range doc.Runs {
		ruleMap := buildRuleMap(run.Tool.Driver.Rules)
		for _, res := range run.Results {
			issues = append(issues, convertResult(res, ruleMap)...)
		}
	}
	return issues, nil
}

// buildRuleMap indexes rules by their ID for quick lookup.
func buildRuleMap(rules []sarifRule) map[string]sarifRule {
	m := make(map[string]sarifRule, len(rules))
	for _, r := range rules {
		m[r.ID] = r
	}
	return m
}

// convertResult maps a single SARIF result to zero or more ODS issues
// (one per location; most results have exactly one).
func convertResult(res sarifResult, ruleMap map[string]sarifRule) []analyzer.Issue {
	severity := levelToSeverity(res.Level)

	// Some tools encode severity in the rule's properties rather than in the
	// result level. Check that as a fallback.
	if rule, ok := ruleMap[res.RuleID]; ok {
		if s := coalesceString(rule.Properties.Severity, rule.Properties.ProblemSeverity); s != "" {
			severity = propSeverityToODS(s)
		}
	}

	ruleID := res.RuleID
	if ruleID == "" {
		ruleID = "sarif-finding"
	}

	msg := res.Message.Text
	if msg == "" {
		if rule, ok := ruleMap[ruleID]; ok {
			msg = rule.ShortDescription.Text
		}
	}
	if msg == "" {
		msg = ruleID
	}

	if len(res.Locations) == 0 {
		return []analyzer.Issue{{
			Rule:     ruleID,
			File:     "",
			Line:     0,
			Severity: severity,
			Message:  msg,
		}}
	}

	issues := make([]analyzer.Issue, 0, len(res.Locations))
	for _, loc := range res.Locations {
		issues = append(issues, analyzer.Issue{
			Rule:     ruleID,
			File:     loc.PhysicalLocation.ArtifactLocation.URI,
			Line:     loc.PhysicalLocation.Region.StartLine,
			Severity: severity,
			Message:  msg,
		})
	}
	return issues
}

// levelToSeverity maps a SARIF result level to an ODS severity string.
func levelToSeverity(level string) string {
	switch level {
	case "error":
		return "high"
	case "warning":
		return "medium"
	case "note":
		return "low"
	default:
		return "info"
	}
}

// propSeverityToODS maps tool-specific property strings to ODS severity values.
func propSeverityToODS(s string) string {
	switch s {
	case "CRITICAL", "critical":
		return "critical"
	case "ERROR", "error", "HIGH", "high":
		return "high"
	case "WARNING", "warning", "MEDIUM", "medium":
		return "medium"
	case "INFO", "info", "LOW", "low":
		return "low"
	default:
		return "info"
	}
}

func coalesceString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
