package review

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "verdict.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const valid = `{
  "schema": "ods.dev/review-verdict/v1",
  "reviewer": {"tool": "claude-code", "model": "claude-sonnet-4-5"},
  "head_sha": "a1b2c3d",
  "verdict": "request_changes",
  "findings": [
    {"file": "src/auth.py", "line": 42, "severity": "high",
     "category": "correctness",
     "message": "Token expiry is never checked before refresh"}
  ]
}`

func TestLoadValid(t *testing.T) {
	v, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if v.Verdict != VerdictRequestChanges || v.Reviewer.Tool != "claude-code" {
		t.Errorf("unexpected verdict: %+v", v)
	}
	if len(v.Findings) != 1 || v.Findings[0].Severity != "high" {
		t.Errorf("unexpected findings: %+v", v.Findings)
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]string{
		"foreign schema":  `{"schema":"other/v9","reviewer":{"tool":"x"},"verdict":"approve"}`,
		"invalid verdict": `{"schema":"ods.dev/review-verdict/v1","reviewer":{"tool":"x"},"verdict":"lgtm"}`,
		"missing tool":    `{"schema":"ods.dev/review-verdict/v1","reviewer":{},"verdict":"approve"}`,
		"not json":        `nonsense{{{`,
	}
	for name, content := range cases {
		if _, err := Load(write(t, content)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("missing file: expected error")
	}
}

func TestMatchesHead(t *testing.T) {
	cases := []struct {
		verdictSHA, head string
		want             bool
	}{
		{"a1b2c3d", "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", true}, // short vs full
		{"a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", "a1b2c3d", true}, // full vs short
		{"a1b2c3d", "a1b2c3d", true},
		{"", "whatever", true},        // unstamped verdict matches
		{"a1b2c3d", "", true},         // no local head to compare
		{"a1b2c3d", "ffffff0", false}, // stale verdict
		{"A1B2C3D", "a1b2c3d", true},  // case-insensitive
	}
	for _, tc := range cases {
		v := &Verdict{HeadSHA: tc.verdictSHA}
		if got := v.MatchesHead(tc.head); got != tc.want {
			t.Errorf("MatchesHead(%q, %q) = %v, want %v", tc.verdictSHA, tc.head, got, tc.want)
		}
	}
}
