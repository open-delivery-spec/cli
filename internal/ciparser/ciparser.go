// Package ciparser provides CI log parsing with AI hallucination detection.
package ciparser

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Failure represents a single test failure.
type Failure struct {
	TestName      string `json:"test_name"`
	TestFile      string `json:"test_file,omitempty"`
	FailureType   string `json:"failure_type"`
	Message       string `json:"message"`
	AIRelated     bool   `json:"ai_related"`
	AIExplanation string `json:"ai_explanation,omitempty"`
	SuggestedFix  string `json:"suggested_fix,omitempty"`
	StackTrace    string `json:"stack_trace,omitempty"`
}

// Hallucination represents a detected AI hallucination.
type Hallucination struct {
	Category            string  `json:"category"`
	File                string  `json:"file"`
	Line                int     `json:"line"`
	Description         string  `json:"description"`
	HallucinatedSymbol  string  `json:"hallucinated_symbol,omitempty"`
	ClosestValidSymbol  string  `json:"closest_valid_symbol,omitempty"`
	LevenshteinDistance int     `json:"levenshtein_distance,omitempty"`
	Confidence          float64 `json:"confidence"`
}

// Stage represents a CI pipeline stage.
type Stage struct {
	Status          string    `json:"status"`
	DurationSeconds float64   `json:"duration_seconds"`
	Failures        []Failure `json:"failures,omitempty"`
}

// AISummary summarizes AI contribution to failures.
type AISummary struct {
	AIContributedStages []string `json:"ai_contributed_stages"`
	LikelyAICaused      bool     `json:"likely_ai_caused"`
	Confidence          string   `json:"confidence"`
	Explanation         string   `json:"explanation,omitempty"`
}

// FixSuggestion is an actionable fix.
type FixSuggestion struct {
	Priority        int    `json:"priority"`
	File            string `json:"file,omitempty"`
	Action          string `json:"action"`
	AutoFixAvailable bool  `json:"auto_fix_available"`
}

// Report is the full CI failure report.
type Report struct {
	PipelineID      string          `json:"pipeline_id"`
	Repository      string          `json:"repository"`
	Branch          string          `json:"branch"`
	Commit          string          `json:"commit"`
	Trigger         string          `json:"trigger,omitempty"`
	Timestamp       string          `json:"timestamp"`
	Status          string          `json:"status"`
	DurationSeconds float64         `json:"duration_seconds,omitempty"`
	Stages          map[string]Stage `json:"stages"`
	AISummary       AISummary       `json:"ai_summary"`
	FixSuggestions  []FixSuggestion  `json:"fix_suggestions,omitempty"`
	Hallucinations  []Hallucination  `json:"hallucinations,omitempty"`
}

// ParseLog parses a raw CI log and produces a structured failure report.
func ParseLog(log string, pipelineID, repo, branch, commit, trigger string) (*Report, error) {
	report := &Report{
		PipelineID: pipelineID,
		Repository: repo,
		Branch:     branch,
		Commit:     commit,
		Trigger:    trigger,
		Timestamp:  timestampNow(),
		Status:     "failed",
		Stages:     make(map[string]Stage),
	}

	lines := strings.Split(log, "\n")
	currentStage := ""
	var failures []Failure
	var stageDuration float64

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Detect stage headers
		if stageName := detectStageHeader(line); stageName != "" {
			if currentStage != "" && len(failures) > 0 {
				report.Stages[currentStage] = Stage{
					Status:          stageStatus(failures),
					DurationSeconds: stageDuration,
					Failures:        failures,
				}
			}
			currentStage = stageName
			failures = nil
			stageDuration = 0
			continue
		}

		// Detect test failures
		if failure := detectFailure(line, currentStage); failure != nil {
			failures = append(failures, *failure)
			continue
		}

		// Track duration
		if d := detectDuration(line); d > 0 {
			stageDuration += d
		}
	}

	// Save last stage (or assign to default if no stage detected)
	if currentStage == "" {
		currentStage = "default"
	}
	if len(failures) > 0 {
		report.Stages[currentStage] = Stage{
			Status:          stageStatus(failures),
			DurationSeconds: stageDuration,
			Failures:        failures,
		}
	}

	// Run hallucination detection
	report.Hallucinations = detectHallucinations(failures)

	// Build AI summary
	report.AISummary = buildAISummary(report.Stages, report.Hallucinations)

	// Generate fix suggestions
	report.FixSuggestions = generateFixes(failures, report.Hallucinations)

	return report, nil
}

// detectStageHeader detects common CI stage names.
func detectStageHeader(line string) string {
	stagePatterns := map[string]*regexp.Regexp{
		"lint":           regexp.MustCompile(`(?i)^(?:\[)?(?:stage:|running)\s*(lint|eslint|golangci-lint|rubocop)`),
		"test":           regexp.MustCompile(`(?i)^(?:\[)?(?:stage:|running)\s*(test|tests|unit-test|integration-test)`),
		"build":          regexp.MustCompile(`(?i)^(?:\[)?(?:stage:|running)\s*(build|compile)`),
		"security_scan":  regexp.MustCompile(`(?i)^(?:\[)?(?:stage:|running)\s*(security|sast|trivy|gosec)`),
		"deploy":         regexp.MustCompile(`(?i)^(?:\[)?(?:stage:|running)\s*(deploy)`),
		"type_check":     regexp.MustCompile(`(?i)^(?:\[)?(?:stage:|running)\s*(type.?check|tsc|mypy)`),
	}

	for name, pattern := range stagePatterns {
		if pattern.MatchString(line) {
			return name
		}
	}

	return ""
}

// detectFailure tries to parse a test failure from a log line.
func detectFailure(line string, stage string) *Failure {
	failurePattern := regexp.MustCompile(`(?i)(FAIL|ERROR|FAILED)\s+(.+?)(?:\s+\((.+?)\))?$`)
	matches := failurePattern.FindStringSubmatch(line)
	if len(matches) < 3 {
		return nil
	}

	testName := strings.TrimSpace(matches[2])
	testFile := ""
	if len(matches) > 3 && matches[3] != "" {
		testFile = strings.TrimSpace(matches[3])
	}

	failureType := classifyFailure(line)
	aiRelated, aiExplanation, suggestedFix := detectAIRelatedFailure(line, testName)

	return &Failure{
		TestName:      testName,
		TestFile:      testFile,
		FailureType:   failureType,
		Message:       line,
		AIRelated:     aiRelated,
		AIExplanation: aiExplanation,
		SuggestedFix:  suggestedFix,
	}
}

func classifyFailure(line string) string {
	classifiers := []struct {
		pattern string
		ftype   string
	}{
		{`(?i)assertion|expected|got|want`, "assertion_error"},
		{`(?i)compilation|undefined|undeclared|not found`, "compilation_error"},
		{`(?i)type error|type mismatch|cannot use`, "type_error"},
		{`(?i)lint|style|convention|vet`, "lint_error"},
		{`(?i)security|vulnerability|cve|injection`, "security_violation"},
		{`(?i)timeout|timed out|deadline exceeded`, "timeout"},
		{`(?i)dependency|package|module|import`, "dependency_error"},
		{`(?i)config|configuration|invalid setting`, "configuration_error"},
		{`(?i)integration|e2e|end.to.end|contract`, "integration_error"},
	}

	for _, c := range classifiers {
		if matched, _ := regexp.MatchString(c.pattern, line); matched {
			return c.ftype
		}
	}
	return "unknown"
}

func detectAIRelatedFailure(line, testName string) (bool, string, string) {
	lower := strings.ToLower(line)
	lowerTest := strings.ToLower(testName)

	indicators := map[string]struct {
		explanation string
		fix         string
	}{
		"hallucinat":    {explanation: "AI hallucinated symbol or API call", fix: "Verify the referenced function/symbol exists in the codebase"},
		"nonexistent":   {explanation: "AI referenced a non-existent function or import", fix: "Check imports and function calls for validity"},
		"deprecated":    {explanation: "AI used deprecated API", fix: "Replace with the current API version"},
		"fake":          {explanation: "AI generated fake or test data that doesn't match real APIs", fix: "Replace mocked data with real API responses or proper fixtures"},
		"wrong url":     {explanation: "AI hallucinated an incorrect URL or endpoint", fix: "Verify against actual API documentation"},
		"undefined":     {explanation: "AI referenced an undefined symbol", fix: "Check if the symbol exists and its correct name"},
		"not a function": {explanation: "AI called something that isn't a function", fix: "Verify the correct function signature"},
	}

	combined := lower + " " + lowerTest
	for keyword, info := range indicators {
		if strings.Contains(combined, keyword) {
			return true, info.explanation, info.fix
		}
	}

	return false, "", ""
}

func detectDuration(line string) float64 {
	durPattern := regexp.MustCompile(`(?i)(\d+\.?\d*)\s*(s|sec|seconds|ms|milliseconds|m|minutes)`)
	matches := durPattern.FindStringSubmatch(line)
	if len(matches) < 3 {
		return 0
	}

	var seconds float64
	fmt.Sscanf(matches[1], "%f", &seconds)
	unit := strings.ToLower(matches[2])

	switch {
	case strings.HasPrefix(unit, "ms"):
		seconds /= 1000
	case strings.HasPrefix(unit, "m"):
		seconds *= 60
	}
	return seconds
}

func stageStatus(failures []Failure) string {
	if len(failures) > 0 {
		return "failed"
	}
	return "passed"
}

func timestampNow() string {
	return "2026-01-15T14:30:00Z" // Placeholder; real implementation uses time.Now().Format(time.RFC3339)
}

// detectHallucinations analyzes failures for AI hallucination patterns.
func detectHallucinations(failures []Failure) []Hallucination {
	var hallucinations []Hallucination

	halPatterns := []struct {
		pattern    *regexp.Regexp
		category   string
		confidence float64
	}{
		{
			pattern:    regexp.MustCompile(`(?i)(?:undefined|undeclared|not found|no such|does not exist|unknown symbol)\s+(?:symbol|function|method|variable|type|class|api|call)?\s*['"]?(\w+)['"]?`),
			category:   "non_existent_symbol",
			confidence: 0.85,
		},
		{
			pattern:    regexp.MustCompile(`(?i)(?:cannot find|no module|package|import)\s+['"]?([\w./-]+)['"]?`),
			category:   "wrong_import",
			confidence: 0.80,
		},
		{
			pattern:    regexp.MustCompile(`(?i)(?:deprecated|deprecated in|has been deprecated)`),
			category:   "deprecated_api",
			confidence: 0.70,
		},
		{
			pattern:    regexp.MustCompile(`(?i)(?:invalid|wrong|incorrect|bad|fake|hallucinated)\s+(url|endpoint|host|domain|address)\s*['"]?([\w./:-]+)['"]?`),
			category:   "fake_url",
			confidence: 0.75,
		},
		{
			pattern:    regexp.MustCompile(`(?i)(?:unexpected|invalid|wrong|bad|hallucinated)\s+(default|config|setting|value)\s*['"]?([\w./-]+)['"]?`),
			category:   "incorrect_defaults",
			confidence: 0.70,
		},
		{
			pattern:    regexp.MustCompile(`(?i)hallucinated\s+(?:function|method|api)\s+(?:call|usage|symbol)?\s*:?\s*['"]?(\w+)['"]?`),
			category:   "hallucinated_call",
			confidence: 0.90,
		},
	}

	for _, f := range failures {
		if !f.AIRelated {
			continue
		}
		for _, p := range halPatterns {
			matches := p.pattern.FindStringSubmatch(f.Message)
			if matches == nil {
				continue
			}

			symbol := ""
			if len(matches) > 1 {
				// Find the last non-empty capture group (varies by pattern)
				for i := len(matches) - 1; i > 0; i-- {
					if matches[i] != "" {
						symbol = matches[i]
						break
					}
				}
			}

			h := Hallucination{
				Category:           p.category,
				File:               f.TestFile,
				Description:        fmt.Sprintf("%s", truncateMessage(f.Message, 120)),
				HallucinatedSymbol: symbol,
				Confidence:         p.confidence,
			}

			// Find closest valid symbol if the error indicates a missing symbol
			if symbol != "" && (strings.Contains(f.Message, "not found") || strings.Contains(f.Message, "does not exist") || strings.Contains(f.Message, "undefined")) {
				// In a real implementation, this would scan the codebase
				h.ClosestValidSymbol = findClosestValid(symbol)
				h.LevenshteinDistance = levenshtein(symbol, h.ClosestValidSymbol)
			}

			hallucinations = append(hallucinations, h)
		}
	}

	return hallucinations
}

func buildAISummary(stages map[string]Stage, hallucinations []Hallucination) AISummary {
	aiStages := []string{}
	likelyCaused := len(hallucinations) > 0
	confidence := "low"

	for name, stage := range stages {
		for _, f := range stage.Failures {
			if f.AIRelated {
				aiStages = append(aiStages, name)
				break
			}
		}
	}

	halCount := len(hallucinations)
	if halCount >= 3 {
		confidence = "high"
	} else if halCount >= 1 {
		confidence = "medium"
	}

	explanation := ""
	if likelyCaused {
		files := []string{}
		for _, h := range hallucinations {
			if h.File != "" {
				files = append(files, h.File)
			}
		}
		explanation = fmt.Sprintf("%d AI hallucination(s) detected across %d stages. Affected files: %s",
			halCount, len(aiStages), strings.Join(unique(files), ", "))
	} else {
		explanation = "No clear AI hallucinations detected in failures."
	}

	return AISummary{
		AIContributedStages: aiStages,
		LikelyAICaused:      likelyCaused,
		Confidence:          confidence,
		Explanation:         explanation,
	}
}

func generateFixes(failures []Failure, hallucinations []Hallucination) []FixSuggestion {
	var fixes []FixSuggestion
	seen := map[string]bool{}

	for i, h := range hallucinations {
		key := h.File + ":" + h.Description
		if seen[key] {
			continue
		}
		seen[key] = true

		autoFix := false
		action := fmt.Sprintf("[%s] %s", categoryLabel(h.Category), h.Description)
		if h.ClosestValidSymbol != "" {
			autoFix = true
			action = fmt.Sprintf("Replace '%s' with '%s' (auto-fix available, distance=%d)", h.HallucinatedSymbol, h.ClosestValidSymbol, h.LevenshteinDistance)
		}
		if h.HallucinatedSymbol != "" && !autoFix {
			action = fmt.Sprintf("[%s] Verify symbol '%s': %s", categoryLabel(h.Category), h.HallucinatedSymbol, h.Description)
		}

		fixes = append(fixes, FixSuggestion{
			Priority:        i + 1,
			File:            h.File,
			Action:          action,
			AutoFixAvailable: autoFix,
		})
	}

	// Add general fix suggestions for non-hallucination AI failures
	for _, f := range failures {
		if f.AIRelated && f.SuggestedFix != "" {
			key := f.TestFile + ":" + f.SuggestedFix
			if seen[key] {
				continue
			}
			seen[key] = true
			fixes = append(fixes, FixSuggestion{
				Priority: len(fixes) + 1,
				File:     f.TestFile,
				Action:   f.SuggestedFix,
			})
		}
	}

	return fixes
}

// findClosestValid finds the closest valid symbol via simplified heuristic.
func findClosestValid(symbol string) string {
	// In production, this would scan the codebase AST for similar symbols
	// Simplified: strip common suffixes/prefixes that AI might add
	normalized := strings.TrimSuffix(symbol, "V2")
	normalized = strings.TrimSuffix(normalized, "V3")
	normalized = strings.TrimSuffix(normalized, "2")
	normalized = strings.TrimSuffix(normalized, "3")
	normalized = strings.TrimPrefix(normalized, "new")
	return normalized
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		dp[0][j] = j
	}

	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			dp[i][j] = min3(
				dp[i-1][j]+1,
				dp[i][j-1]+1,
				dp[i-1][j-1]+cost,
			)
		}
	}
	return dp[la][lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

func categoryLabel(category string) string {
	labels := map[string]string{
		"non_existent_symbol": "Hallucinated symbol",
		"wrong_import":        "Wrong import",
		"deprecated_api":      "Deprecated API",
		"fake_url":            "Fake URL",
		"incorrect_defaults":  "Wrong default",
		"hallucinated_call":   "Hallucinated call",
	}
	if label, ok := labels[category]; ok {
		return label
	}
	return category
}

func truncateMessage(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen-3] + "..."
}

func unique(items []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

// ToJSON marshals a report to JSON.
func (r *Report) ToJSON() (string, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
