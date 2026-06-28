package sarif

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTemp writes content to a temp file and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "results.sarif")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp SARIF: %v", err)
	}
	return path
}

// semgrepSARIF is a minimal but realistic semgrep-style document: severity lives
// in the result level and is mirrored in the rule's properties.severity.
const semgrepSARIF = `{
  "version": "2.1.0",
  "runs": [
    {
      "tool": {
        "driver": {
          "name": "semgrep",
          "rules": [
            {
              "id": "go.lang.security.audit.dangerous-exec",
              "shortDescription": {"text": "Dangerous exec call"},
              "properties": {"severity": "ERROR"}
            }
          ]
        }
      },
      "results": [
        {
          "ruleId": "go.lang.security.audit.dangerous-exec",
          "level": "error",
          "message": {"text": "Detected subprocess call with untrusted input"},
          "locations": [
            {
              "physicalLocation": {
                "artifactLocation": {"uri": "internal/cmd/run.go"},
                "region": {"startLine": 42}
              }
            }
          ]
        }
      ]
    }
  ]
}`

func TestLoad_Semgrep(t *testing.T) {
	path := writeTemp(t, semgrepSARIF)
	issues, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}
	got := issues[0]
	if got.Rule != "go.lang.security.audit.dangerous-exec" {
		t.Errorf("Rule = %q", got.Rule)
	}
	if got.File != "internal/cmd/run.go" {
		t.Errorf("File = %q, want internal/cmd/run.go", got.File)
	}
	if got.Line != 42 {
		t.Errorf("Line = %d, want 42", got.Line)
	}
	// properties.severity "ERROR" overrides level "error" — both map to high here.
	if got.Severity != "high" {
		t.Errorf("Severity = %q, want high", got.Severity)
	}
	if got.Message != "Detected subprocess call with untrusted input" {
		t.Errorf("Message = %q", got.Message)
	}
}

// codeqlSARIF uses CodeQL conventions: severity is in properties["problem.severity"]
// rather than properties.severity.
const codeqlSARIF = `{
  "version": "2.1.0",
  "runs": [
    {
      "tool": {
        "driver": {
          "name": "CodeQL",
          "rules": [
            {
              "id": "js/sql-injection",
              "shortDescription": {"text": "SQL injection"},
              "properties": {"problem.severity": "critical"}
            }
          ]
        }
      },
      "results": [
        {
          "ruleId": "js/sql-injection",
          "level": "warning",
          "message": {"text": "User input flows into SQL query"},
          "locations": [
            {
              "physicalLocation": {
                "artifactLocation": {"uri": "src/db.js"},
                "region": {"startLine": 100}
              }
            }
          ]
        }
      ]
    }
  ]
}`

func TestLoad_CodeQLProblemSeverity(t *testing.T) {
	path := writeTemp(t, codeqlSARIF)
	issues, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}
	// problem.severity "critical" must override the result level "warning" (medium).
	if issues[0].Severity != "critical" {
		t.Errorf("Severity = %q, want critical (problem.severity should override level)", issues[0].Severity)
	}
}

func TestLoad_LevelToSeverityFallback(t *testing.T) {
	// A run whose rules carry no severity property: severity must come from level.
	doc := `{
      "version": "2.1.0",
      "runs": [{
        "tool": {"driver": {"name": "tool", "rules": []}},
        "results": [
          {"ruleId": "r-error",   "level": "error",   "message": {"text": "m"}, "locations": [{"physicalLocation": {"artifactLocation": {"uri": "a"}, "region": {"startLine": 1}}}]},
          {"ruleId": "r-warning", "level": "warning", "message": {"text": "m"}, "locations": [{"physicalLocation": {"artifactLocation": {"uri": "b"}, "region": {"startLine": 1}}}]},
          {"ruleId": "r-note",    "level": "note",    "message": {"text": "m"}, "locations": [{"physicalLocation": {"artifactLocation": {"uri": "c"}, "region": {"startLine": 1}}}]},
          {"ruleId": "r-none",    "level": "none",    "message": {"text": "m"}, "locations": [{"physicalLocation": {"artifactLocation": {"uri": "d"}, "region": {"startLine": 1}}}]}
        ]
      }]
    }`
	path := writeTemp(t, doc)
	issues, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	want := map[string]string{
		"r-error":   "high",
		"r-warning": "medium",
		"r-note":    "low",
		"r-none":    "info",
	}
	if len(issues) != len(want) {
		t.Fatalf("len(issues) = %d, want %d", len(issues), len(want))
	}
	for _, iss := range issues {
		if want[iss.Rule] != iss.Severity {
			t.Errorf("rule %s: severity = %q, want %q", iss.Rule, iss.Severity, want[iss.Rule])
		}
	}
}

func TestLoad_EmptyResults(t *testing.T) {
	doc := `{"version": "2.1.0", "runs": [{"tool": {"driver": {"name": "tool"}}, "results": []}]}`
	path := writeTemp(t, doc)
	issues, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("len(issues) = %d, want 0", len(issues))
	}
}

func TestLoad_NoRuns(t *testing.T) {
	path := writeTemp(t, `{"version": "2.1.0", "runs": []}`)
	issues, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("len(issues) = %d, want 0", len(issues))
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.sarif"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	path := writeTemp(t, "this is not json {{{")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoad_NoLocations(t *testing.T) {
	// A result with no locations should still yield one issue (file empty, line 0).
	doc := `{
      "version": "2.1.0",
      "runs": [{
        "tool": {"driver": {"name": "tool"}},
        "results": [{"ruleId": "project-wide", "level": "warning", "message": {"text": "global finding"}}]
      }]
    }`
	path := writeTemp(t, doc)
	issues, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}
	if issues[0].File != "" || issues[0].Line != 0 {
		t.Errorf("File/Line = %q/%d, want \"\"/0", issues[0].File, issues[0].Line)
	}
}

func TestLoad_MultipleLocations(t *testing.T) {
	// One result with two locations should expand to two issues.
	doc := `{
      "version": "2.1.0",
      "runs": [{
        "tool": {"driver": {"name": "tool"}},
        "results": [{
          "ruleId": "dup",
          "level": "error",
          "message": {"text": "duplicate block"},
          "locations": [
            {"physicalLocation": {"artifactLocation": {"uri": "a.go"}, "region": {"startLine": 10}}},
            {"physicalLocation": {"artifactLocation": {"uri": "b.go"}, "region": {"startLine": 20}}}
          ]
        }]
      }]
    }`
	path := writeTemp(t, doc)
	issues, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("len(issues) = %d, want 2", len(issues))
	}
	if issues[0].File != "a.go" || issues[1].File != "b.go" {
		t.Errorf("files = %q, %q; want a.go, b.go", issues[0].File, issues[1].File)
	}
}

func TestLoad_EmptyRuleIDDefaults(t *testing.T) {
	doc := `{
      "version": "2.1.0",
      "runs": [{
        "tool": {"driver": {"name": "tool"}},
        "results": [{"level": "error", "message": {"text": "no rule id"}, "locations": [{"physicalLocation": {"artifactLocation": {"uri": "a.go"}, "region": {"startLine": 1}}}]}]
      }]
    }`
	path := writeTemp(t, doc)
	issues, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}
	if issues[0].Rule != "sarif-finding" {
		t.Errorf("Rule = %q, want sarif-finding", issues[0].Rule)
	}
}

func TestLoad_MessageFallsBackToRuleDescription(t *testing.T) {
	// Result has no message text; it should fall back to the rule's shortDescription.
	doc := `{
      "version": "2.1.0",
      "runs": [{
        "tool": {"driver": {"name": "tool", "rules": [
          {"id": "r1", "shortDescription": {"text": "Rule level description"}}
        ]}},
        "results": [{"ruleId": "r1", "level": "warning", "locations": [{"physicalLocation": {"artifactLocation": {"uri": "a.go"}, "region": {"startLine": 1}}}]}]
      }]
    }`
	path := writeTemp(t, doc)
	issues, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}
	if issues[0].Message != "Rule level description" {
		t.Errorf("Message = %q, want rule shortDescription fallback", issues[0].Message)
	}
}

func TestLoad_MessageFallsBackToRuleID(t *testing.T) {
	// No message text and no rule description: message should default to the rule ID.
	doc := `{
      "version": "2.1.0",
      "runs": [{
        "tool": {"driver": {"name": "tool"}},
        "results": [{"ruleId": "bare-rule", "level": "note", "locations": [{"physicalLocation": {"artifactLocation": {"uri": "a.go"}, "region": {"startLine": 1}}}]}]
      }]
    }`
	path := writeTemp(t, doc)
	issues, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if issues[0].Message != "bare-rule" {
		t.Errorf("Message = %q, want bare-rule (rule ID fallback)", issues[0].Message)
	}
}

func TestLevelToSeverity(t *testing.T) {
	cases := map[string]string{
		"error":   "high",
		"warning": "medium",
		"note":    "low",
		"none":    "info",
		"":        "info",
		"unknown": "info",
	}
	for level, want := range cases {
		if got := levelToSeverity(level); got != want {
			t.Errorf("levelToSeverity(%q) = %q, want %q", level, got, want)
		}
	}
}

func TestPropSeverityToODS(t *testing.T) {
	cases := map[string]string{
		"CRITICAL": "critical",
		"critical": "critical",
		"ERROR":    "high",
		"HIGH":     "high",
		"high":     "high",
		"WARNING":  "medium",
		"MEDIUM":   "medium",
		"medium":   "medium",
		"INFO":     "low",
		"LOW":      "low",
		"low":      "low",
		"":         "info",
		"weird":    "info",
	}
	for in, want := range cases {
		if got := propSeverityToODS(in); got != want {
			t.Errorf("propSeverityToODS(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCoalesceString(t *testing.T) {
	if got := coalesceString("", "", "third"); got != "third" {
		t.Errorf("coalesceString = %q, want third", got)
	}
	if got := coalesceString("first", "second"); got != "first" {
		t.Errorf("coalesceString = %q, want first", got)
	}
	if got := coalesceString("", ""); got != "" {
		t.Errorf("coalesceString = %q, want empty", got)
	}
}

// realSemgrepSARIF mirrors what `semgrep --sarif` actually emits: the result has
// NO level, and severity lives in the rule's defaultConfiguration.level.
const realSemgrepSARIF = `{
  "version": "2.1.0",
  "runs": [
    {
      "tool": {
        "driver": {
          "name": "semgrep",
          "rules": [
            {
              "id": "python.subprocess-shell-true",
              "defaultConfiguration": {"level": "error"},
              "properties": {"precision": "very-high"}
            }
          ]
        }
      },
      "results": [
        {
          "ruleId": "python.subprocess-shell-true",
          "message": {"text": "shell=True on untrusted input"},
          "locations": [
            {"physicalLocation": {"artifactLocation": {"uri": "app/runner.py"}, "region": {"startLine": 11}}}
          ]
        }
      ]
    }
  ]
}`

func TestLoad_DefaultConfigurationLevel(t *testing.T) {
	issues, err := Load(writeTemp(t, realSemgrepSARIF))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}
	// error in defaultConfiguration must map to high (not info).
	if issues[0].Severity != "high" {
		t.Errorf("Severity = %q, want high (from defaultConfiguration.level=error)", issues[0].Severity)
	}
}

func TestLoad_SecuritySeverityScore(t *testing.T) {
	doc := `{
  "version": "2.1.0",
  "runs": [{
    "tool": {"driver": {"name": "codeql", "rules": [
      {"id": "js/sql-injection", "properties": {"security-severity": "9.8"}}
    ]}},
    "results": [{
      "ruleId": "js/sql-injection",
      "message": {"text": "SQL injection"},
      "locations": [{"physicalLocation": {"artifactLocation": {"uri": "a.js"}, "region": {"startLine": 1}}}]
    }]
  }]
}`
	issues, err := Load(writeTemp(t, doc))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(issues) != 1 || issues[0].Severity != "critical" {
		t.Fatalf("severity = %v, want critical (security-severity 9.8)", issues)
	}
}

func TestSecurityScoreToODS(t *testing.T) {
	cases := map[string]string{"9.9": "critical", "7.0": "high", "4.5": "medium", "1.0": "low", "0": "info", "bad": "info"}
	for in, want := range cases {
		if got := securityScoreToODS(in); got != want {
			t.Errorf("securityScoreToODS(%q) = %q, want %q", in, got, want)
		}
	}
}
