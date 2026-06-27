package analyzer

import (
	"strings"
	"testing"
)

func TestCheckRedundantErrorHandling(t *testing.T) {
	t.Run("consecutive simple returns", func(t *testing.T) {
		lines := []string{
			"data, err := fetchData()",
			"if err != nil {",
			"    return err",
			"}",
			"result, err := process(data)",
			"if err != nil {",
			"    return err",
			"}",
		}
		issues := checkRedundantErrorHandling("test.go", lines)
		if len(issues) == 0 {
			t.Fatal("expected redundant error handling issue, got none")
		}
		if issues[0].Rule != "ai-redundant-error-handling" {
			t.Errorf("rule = %s, want ai-redundant-error-handling", issues[0].Rule)
		}
	})

	t.Run("consecutive blocks with different handling", func(t *testing.T) {
		lines := []string{
			"data, err := fetchData()",
			"if err != nil {",
			"    log.Printf(\"fetch error: %v\", err)",
			"    return fmt.Errorf(\"fetch: %w\", err)",
			"}",
			"result, err := process(data)",
			"if err != nil {",
			"    return err",
			"}",
		}
		issues := checkRedundantErrorHandling("test.go", lines)
		// First block wraps error, second just returns — same pattern detected
		if len(issues) == 0 {
			t.Fatal("expected redundant error handling issue for wrapped+simple pattern")
		}
	})

	t.Run("no redundant pattern", func(t *testing.T) {
		lines := []string{
			"data, err := fetchData()",
			"if err != nil {",
			"    return err",
			"}",
			"x := 1",
			"y := 2",
			"z := 3",
			"w := 4",
			"v := 5",
			"u := 6",
			"result, err := process(data)",
			"if err != nil {",
			"    return err",
			"}",
		}
		issues := checkRedundantErrorHandling("test.go", lines)
		if len(issues) > 0 {
			t.Errorf("expected no issues for spaced-out error blocks, got %d", len(issues))
		}
	})
}

func TestCheckOverCommenting(t *testing.T) {
	t.Run("high comment ratio", func(t *testing.T) {
		lines := []string{
			"// This function handles user authentication",
			"// It validates the token and checks expiry",
			"// Returns an error if authentication fails",
			"// We log all authentication attempts",
			"// This is important for audit compliance",
			"// Session tokens are stored in Redis for fast lookup",
			"func auth(token string) error {",
			"    if token == \"\" {",
			"        return errors.New(\"empty\")",
			"    }",
			"    return nil",
			"}",
		}
		issues := checkOverCommenting("test.go", lines)
		if len(issues) == 0 {
			t.Fatal("expected over-commenting issue, got none")
		}
		// Over-commenting is a style hint, always informational — it must never
		// be promoted to a blocking severity (regression guard).
		if issues[0].Severity != "info" {
			t.Errorf("severity = %s, want info", issues[0].Severity)
		}
	})

	t.Run("normal comment ratio", func(t *testing.T) {
		lines := []string{
			"func add(a, b int) int {",
			"    return a + b",
			"}",
			"func sub(a, b int) int {",
			"    return a - b",
			"}",
			"func mul(a, b int) int {",
			"    return a * b",
			"}",
		}
		issues := checkOverCommenting("test.go", lines)
		if len(issues) > 0 {
			t.Errorf("expected no issues for normal code, got %d", len(issues))
		}
	})
}

func TestCheckUnsafeDeserialization(t *testing.T) {
	t.Run("unsafe unmarshal", func(t *testing.T) {
		lines := []string{
			"var data interface{}",
			"json.Unmarshal(body, &data)",
		}
		issues := checkUnsafeDeserialization("test.go", lines)
		if len(issues) == 0 {
			t.Fatal("expected unsafe deserialization issue")
		}
		if issues[0].Severity != "high" {
			t.Errorf("severity = %s, want high", issues[0].Severity)
		}
	})

	t.Run("safe unmarshal with struct", func(t *testing.T) {
		lines := []string{
			"var user User",
			"json.Unmarshal(body, &user)",
		}
		issues := checkUnsafeDeserialization("test.go", lines)
		if len(issues) > 0 {
			t.Errorf("expected no issues for typed unmarshal, got %d", len(issues))
		}
	})
}

func TestCheckInconsistentPattern(t *testing.T) {
	t.Run("mixed naming conventions", func(t *testing.T) {
		lines := []string{
			"userName := \"alice\"",
			"user_email := \"alice@example.com\"",
			"userAge := 30",
			"user_address := \"123 Main St\"",
			"displayName := \"Alice\"",
			"phone_number := \"555-0123\"",
		}
		issues := checkInconsistentPattern("test.go", lines)
		found := false
		for _, issue := range issues {
			if strings.Contains(issue.Message, "Mixed naming conventions") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected mixed naming convention issue")
		}
	})

	t.Run("consistent naming", func(t *testing.T) {
		lines := []string{
			"userName := \"alice\"",
			"userAge := 30",
			"displayName := \"Alice\"",
			"phoneNumber := \"555-0123\"",
			"emailAddress := \"a@b.com\"",
		}
		issues := checkInconsistentPattern("test.go", lines)
		for _, issue := range issues {
			if strings.Contains(issue.Message, "Mixed naming conventions") {
				t.Error("expected no mixed naming issue for consistent camelCase")
			}
		}
	})
}

func TestAnalyzeAllRules(t *testing.T) {
	t.Run("AI-like code triggers multiple rules", func(t *testing.T) {
		files := map[string][]string{
			"auth.go": {
				"// This function handles authentication",
				"// It validates the user credentials",
				"// Returns a token on success",
				"// The token expires after 24 hours",
				"// We store it in a secure cookie",
				"func authenticate_user(credentials interface{}) error {",
				"    var data interface{}",
				"    json.Unmarshal(nil, &data)",
				"    if err != nil {",
				"        return err",
				"    }",
				"    if err != nil {",
				"        return err",
				"    }",
				"    return nil",
				"}",
			},
		}

		result := Analyze(Options{Files: files})
		if len(result.Issues) == 0 {
			t.Error("expected multiple issues for AI-like code")
		}

		counts := result.IssueCounts()
		t.Logf("Issue counts: %v", counts)
		t.Logf("Issues: %+v", result.Issues)
	})

	t.Run("clean code produces no issues", func(t *testing.T) {
		files := map[string][]string{
			"math.go": {
				"func add(a, b int) int { return a + b }",
				"func sub(a, b int) int { return a - b }",
				"func mul(a, b int) int { return a * b }",
			},
		}

		result := Analyze(Options{Files: files})
		if len(result.Issues) > 0 {
			t.Errorf("expected no issues for clean code, got %d", len(result.Issues))
		}
	})
}

func TestAnalysisResultHelpers(t *testing.T) {
	r := &AnalysisResult{
		TotalLines: 100,
		Issues: []Issue{
			{Rule: "test", Severity: "critical", Line: 1},
			{Rule: "test", Severity: "high", Line: 2},
			{Rule: "test", Severity: "medium", Line: 3},
			{Rule: "test", Severity: "low", Line: 4},
			{Rule: "test", Severity: "info", Line: 5},
		},
	}

	if r.CriticalCount() != 2 {
		t.Errorf("CriticalCount = %d, want 2", r.CriticalCount())
	}
	if !r.HasCritical() {
		t.Error("HasCritical should be true")
	}
	if !r.HasHigh() {
		t.Error("HasHigh should be true")
	}
	if r.IssueDensity() != 50.0 {
		t.Errorf("IssueDensity = %f, want 50.0", r.IssueDensity())
	}

	counts := r.IssueCounts()
	if counts["critical"] != 1 || counts["high"] != 1 || counts["medium"] != 1 {
		t.Errorf("IssueCounts = %v, want 1 each", counts)
	}
}

func TestSummarizeIssues(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s := summarizeIssues(nil)
		if s != "No quality issues detected" {
			t.Errorf("summary = %s, want 'No quality issues detected'", s)
		}
	})

	t.Run("mixed severities", func(t *testing.T) {
		issues := []Issue{
			{Severity: "critical"},
			{Severity: "high"},
			{Severity: "high"},
			{Severity: "medium"},
		}
		s := summarizeIssues(issues)
		if !strings.Contains(s, "1 critical") || !strings.Contains(s, "2 high") {
			t.Errorf("summary = %s, missing severity counts", s)
		}
	})
}

func TestIsSimpleErrReturn(t *testing.T) {
	lines := []string{
		"if err != nil {",
		"    return err",
		"}",
	}
	if !isSimpleErrReturn(lines, 0) {
		t.Error("isSimpleErrReturn should be true for 'return err'")
	}

	lines2 := []string{
		"if err != nil {",
		"    log.Printf(\"error: %v\", err)",
		"    return fmt.Errorf(\"wrap: %w\", err)",
		"}",
	}
	if !isSimpleErrReturn(lines2, 0) {
		t.Error("isSimpleErrReturn should be true — block contains a return statement")
	}
}
