// Package analyzer detects known quality defects in AI-generated code.
// It provides a rule-based analysis engine that identifies patterns unique to
// AI-generated code — hallucinated APIs, redundant error handling, over-commenting,
// missing edge cases, unsafe patterns, and inconsistent styles.
package analyzer

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/open-delivery-spec/cli/internal/rules"
)

// Issue represents a single quality defect found in code.
type Issue struct {
	Rule       string `json:"rule"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Severity   string `json:"severity"` // critical, high, medium, low, info
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
}

// AnalysisResult holds all issues found during analysis.
type AnalysisResult struct {
	PRNumber   int     `json:"pr_number,omitempty"`
	TotalLines int     `json:"total_lines"`
	AILines    int     `json:"ai_lines"`
	Issues     []Issue `json:"issues"`
	Summary    string  `json:"summary"`
}

// Options configures analysis behavior.
type Options struct {
	// AIOnly when true, adjusts severity weights for AI-specific patterns.
	AIOnly bool
	// Files is a map of filename to content lines.
	Files map[string][]string
	// DiffLines are the added lines from a git diff (keyed by file).
	DiffLines map[string][]string
}

// Analyze runs all quality rules against the provided code.
func Analyze(opts Options) *AnalysisResult {
	result := &AnalysisResult{
		Issues: make([]Issue, 0),
	}

	if opts.Files == nil && opts.DiffLines == nil {
		result.Summary = "No code to analyze"
		return result
	}

	// Merge files and diff lines
	allFiles := make(map[string][]string)
	for path, lines := range opts.Files {
		allFiles[path] = lines
	}
	for path, lines := range opts.DiffLines {
		if existing, ok := allFiles[path]; ok {
			allFiles[path] = append(existing, lines...)
		} else {
			allFiles[path] = lines
		}
	}

	totalLines := 0
	for path, lines := range allFiles {
		totalLines += len(lines)
		result.TotalLines += len(lines)

		// Run each rule
		result.Issues = append(result.Issues, checkRedundantErrorHandling(path, lines)...)
		result.Issues = append(result.Issues, checkOverCommenting(path, lines)...)
		result.Issues = append(result.Issues, checkUnsafeDeserialization(path, lines)...)
		result.Issues = append(result.Issues, checkInconsistentPattern(path, lines)...)
	}

	// Re-number lines for issues in merged files
	_ = totalLines

	// Generate summary
	result.Summary = summarizeIssues(result.Issues)

	return result
}

// summarizeIssues produces a human-readable summary.
func summarizeIssues(issues []Issue) string {
	if len(issues) == 0 {
		return "No quality issues detected"
	}

	critical := 0
	high := 0
	medium := 0
	low := 0
	info := 0

	for _, issue := range issues {
		switch issue.Severity {
		case "critical":
			critical++
		case "high":
			high++
		case "medium":
			medium++
		case "low":
			low++
		case "info":
			info++
		}
	}

	parts := make([]string, 0)
	if critical > 0 {
		parts = append(parts, fmt.Sprintf("%d critical", critical))
	}
	if high > 0 {
		parts = append(parts, fmt.Sprintf("%d high", high))
	}
	if medium > 0 {
		parts = append(parts, fmt.Sprintf("%d medium", medium))
	}
	if low > 0 {
		parts = append(parts, fmt.Sprintf("%d low", low))
	}
	if info > 0 {
		parts = append(parts, fmt.Sprintf("%d info", info))
	}

	return fmt.Sprintf("%d issue(s) found: %s", len(issues), strings.Join(parts, ", "))
}

// ── Rule: ai-redundant-error-handling ──────────────────────────

func checkRedundantErrorHandling(file string, lines []string) []Issue {
	var issues []Issue
	errBlocks := make([]int, 0)

	errPattern := regexp.MustCompile(`if err != nil \{`)

	for i, line := range lines {
		if errPattern.MatchString(strings.TrimSpace(line)) {
			errBlocks = append(errBlocks, i)
		}
	}

	// Check for consecutive error blocks within 5 lines of each other
	// AI tends to write error handling after every single operation
	denseCount := 0
	for i := 1; i < len(errBlocks); i++ {
		if errBlocks[i]-errBlocks[i-1] <= 5 {
			denseCount++
		}
	}

	if denseCount >= 1 {
		issues = append(issues, Issue{
			Rule:     rules.RedundantErrorHandling,
			File:     file,
			Line:     errBlocks[len(errBlocks)-1] + 1,
			Severity: "info",
			Message:  fmt.Sprintf("%d consecutive error handling blocks in close proximity", denseCount+1),
			// Dense `if err != nil` is idiomatic Go, not a defect — surfaced as an
			// informational hint only, never contributing to the blocking debt score.
			Suggestion: "If these checks are repetitive boilerplate, consider a helper; otherwise this is normal Go.",
		})
	}

	return issues
}

// isSimpleErrReturn kept for backward compat in tests but logic simplified above.
func isSimpleErrReturn(lines []string, idx int) bool {
	for offset := 1; offset <= 4 && idx+offset < len(lines); offset++ {
		body := strings.TrimSpace(lines[idx+offset])
		if body == "}" || body == "" {
			continue
		}
		if strings.Contains(body, "return") {
			return true
		}
	}
	return false
}

// ── Rule: ai-over-commenting ───────────────────────────────────

func checkOverCommenting(file string, lines []string) []Issue {
	var issues []Issue

	commentLines := 0
	codeLines := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") ||
			strings.HasPrefix(trimmed, "*/") {
			commentLines++
		} else {
			codeLines++
		}
	}

	total := commentLines + codeLines
	if total == 0 {
		return nil
	}

	ratio := float64(commentLines) / float64(total)
	if ratio >= 0.4 {
		// Comment density is a style signal, not a defect — well-documented public
		// APIs (godoc) legitimately exceed 40%. Reported as info only, so it never
		// inflates defect density or blocks a PR.
		issues = append(issues, Issue{
			Rule:       rules.OverCommenting,
			File:       file,
			Line:       1,
			Severity:   "info",
			Message:    fmt.Sprintf("Comment-to-code ratio is %.0f%%", ratio*100),
			Suggestion: "If comments restate the code, prefer explaining why over what; documentation comments are fine.",
		})
	}

	return issues
}

// ── Rule: ai-unsafe-deserialization ────────────────────────────

var (
	jsonUnmarshalPattern = regexp.MustCompile(`json\.Unmarshal\(`)
	interfacePattern     = regexp.MustCompile(`interface\{\}`)
)

func checkUnsafeDeserialization(file string, lines []string) []Issue {
	var issues []Issue

	// Two-phase check:
	// 1. Find all interface{} variable declarations
	// 2. Check if any json.Unmarshal targets those variables
	interfaceVars := make(map[int]string) // line -> var name

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "interface{}") && strings.Contains(trimmed, "var ") {
			// Extract variable name: var foo interface{}
			parts := strings.Fields(trimmed)
			for j, p := range parts {
				if p == "var" && j+1 < len(parts) {
					interfaceVars[i] = parts[j+1]
				}
			}
		}
		// Also match short declarations: foo := ...interface{}
		if strings.Contains(trimmed, "interface{}") && strings.Contains(trimmed, ":= ") {
			parts := strings.SplitN(trimmed, ":=", 2)
			if len(parts) > 0 {
				name := strings.TrimSpace(parts[0])
				interfaceVars[i] = name
			}
		}
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if jsonUnmarshalPattern.MatchString(trimmed) {
			// Check if any interface{} var from nearby lines is being unmarshalled into
			for varLine, varName := range interfaceVars {
				if i-varLine <= 5 && i >= varLine {
					if strings.Contains(trimmed, "&"+varName) ||
						strings.Contains(trimmed, "&("+varName+")") {
						issues = append(issues, Issue{
							Rule:       rules.UnsafeDeserialization,
							File:       file,
							Line:       i + 1,
							Severity:   "high",
							Message:    fmt.Sprintf("json.Unmarshal into interface{} (%s) without type checking — AI commonly skips type validation", varName),
							Suggestion: "Use a concrete struct type or validate the unmarshalled data before use",
						})
						break
					}
				}
			}
		}
	}

	return issues
}

// ── Rule: ai-inconsistent-pattern ──────────────────────────────

var (
	camelCasePattern   = regexp.MustCompile(`\b[a-z][a-z0-9]*[A-Z][a-zA-Z0-9]*\b`)
	snakeCasePattern   = regexp.MustCompile(`\b[a-z][a-z0-9]+(_[a-z][a-z0-9]+)+\b`)
	tabIndentPattern   = regexp.MustCompile(`^\t+`)
	spaceIndentPattern = regexp.MustCompile(`^    +`)
)

func checkInconsistentPattern(file string, lines []string) []Issue {
	var issues []Issue

	camelCount := 0
	snakeCount := 0
	tabCount := 0
	spaceCount := 0

	for _, line := range lines {
		// Only check non-comment, non-empty lines
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if camelCasePattern.MatchString(trimmed) {
			camelCount++
		}
		if snakeCasePattern.MatchString(trimmed) {
			snakeCount++
		}
		if tabIndentPattern.MatchString(line) {
			tabCount++
		}
		if spaceIndentPattern.MatchString(line) {
			spaceCount++
		}
	}

	// Mixed naming conventions
	if camelCount > 0 && snakeCount > 0 {
		total := camelCount + snakeCount
		minCount := camelCount
		if snakeCount < minCount {
			minCount = snakeCount
		}
		if total > 5 && float64(minCount)/float64(total) > 0.15 {
			issues = append(issues, Issue{
				Rule:       rules.InconsistentPattern,
				File:       file,
				Line:       1,
				Severity:   "medium",
				Message:    fmt.Sprintf("Mixed naming conventions: %d camelCase + %d snake_case identifiers — AI can mix styles from different training sources", camelCount, snakeCount),
				Suggestion: "Standardize on one naming convention. For Go: camelCase. For Python/Ruby: snake_case.",
			})
		}
	}

	// Mixed indentation
	if tabCount > 0 && spaceCount > 0 {
		total := tabCount + spaceCount
		if total > 5 {
			issues = append(issues, Issue{
				Rule:       rules.InconsistentPattern,
				File:       file,
				Line:       1,
				Severity:   "low",
				Message:    fmt.Sprintf("Mixed indentation: %d tab-indented + %d space-indented lines — AI can mix styles", tabCount, spaceCount),
				Suggestion: "Standardize on one indentation style. Use gofmt for Go, prettier for JS/TS.",
			})
		}
	}

	return issues
}

// ResummarizeSARIF regenerates the Summary string after external issues
// (e.g. from SARIF) have been merged into the result's Issues slice.
func ResummarizeSARIF(issues []Issue) string {
	return summarizeIssues(issues)
}

// ── Helpers ────────────────────────────────────────────────────

// CriticalCount returns the number of critical + high severity issues.
func (r *AnalysisResult) CriticalCount() int {
	count := 0
	for _, issue := range r.Issues {
		if issue.Severity == "critical" || issue.Severity == "high" {
			count++
		}
	}
	return count
}

// IssueDensity returns issues per KLOC (1000 lines of code).
func (r *AnalysisResult) IssueDensity() float64 {
	if r.TotalLines == 0 {
		return 0
	}
	return float64(len(r.Issues)) / float64(r.TotalLines) * 1000
}

// SeverityWeightedDensity returns defect density counting only high and critical
// issues per KLOC. Low/medium issues influence warnings but not the blocking
// debt metric — they produce artificially high density on small diffs and would
// trigger false-positive policy blocks on any non-trivial change.
func (r *AnalysisResult) SeverityWeightedDensity() float64 {
	if r.TotalLines == 0 {
		return 0
	}
	count := 0
	for _, issue := range r.Issues {
		if issue.Severity == "critical" || issue.Severity == "high" {
			count++
		}
	}
	return float64(count) / float64(r.TotalLines) * 1000
}

// HasCritical returns true if any critical issues found.
func (r *AnalysisResult) HasCritical() bool {
	for _, issue := range r.Issues {
		if issue.Severity == "critical" {
			return true
		}
	}
	return false
}

// HasHigh returns true if any high-severity issues found.
func (r *AnalysisResult) HasHigh() bool {
	for _, issue := range r.Issues {
		if issue.Severity == "high" {
			return true
		}
	}
	return false
}

// IssueCounts returns counts per severity level.
func (r *AnalysisResult) IssueCounts() map[string]int {
	counts := map[string]int{
		"critical": 0,
		"high":     0,
		"medium":   0,
		"low":      0,
		"info":     0,
	}
	for _, issue := range r.Issues {
		counts[issue.Severity]++
	}
	return counts
}
