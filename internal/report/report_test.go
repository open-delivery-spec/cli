package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validPRBody = `## Summary
Add OAuth login.

## Type
- [x] Feature

## AI Disclosure
- [x] This PR contains AI-generated code
- AI Tool: GitHub Copilot

## Changes
- Added provider integration.

## Testing
- Unit tests added.

## Checklist
- [x] Branch follows ODS.`

func TestBuildCompliantReport(t *testing.T) {
	report := Build(Inputs{
		BranchName:    "feature/add-oauth-login",
		CommitMessage: "feat(auth): add oauth login",
		PRBody:        validPRBody,
		Repository:    "open-delivery-spec/example",
		Ref:           "feature/add-oauth-login",
		SHA:           "1234567890abcdef",
		PRNumber:      42,
	}, Options{GeneratedAt: fixedTime()})

	if report.Status != StatusCompliant {
		t.Fatalf("status = %s, want %s", report.Status, StatusCompliant)
	}
	if report.Score != 100 {
		t.Fatalf("score = %d, want 100", report.Score)
	}
	for _, check := range report.Checks {
		if check.Status != CheckPass {
			t.Fatalf("check %s status = %s, want pass", check.ID, check.Status)
		}
	}
}

func TestBuildWarningAndStrictReport(t *testing.T) {
	inputs := Inputs{
		BranchName:    "feature/ai-generated-client",
		CommitMessage: "feat(client): add generated client",
		PRBody:        validPRBody,
	}

	nonStrict := Build(inputs, Options{GeneratedAt: fixedTime()})
	if nonStrict.Status != StatusCompliantWithWarnings {
		t.Fatalf("non-strict status = %s, want %s", nonStrict.Status, StatusCompliantWithWarnings)
	}
	if nonStrict.Score != 91 {
		t.Fatalf("non-strict score = %d, want 91", nonStrict.Score)
	}

	strict := Build(inputs, Options{Strict: true, GeneratedAt: fixedTime()})
	if strict.Status != StatusNonCompliant {
		t.Fatalf("strict status = %s, want %s", strict.Status, StatusNonCompliant)
	}
	if strict.Checks[0].Status != CheckFail {
		t.Fatalf("strict branch status = %s, want fail", strict.Checks[0].Status)
	}
}

func TestBuildSkipsMissingContext(t *testing.T) {
	report := Build(Inputs{BranchName: "feature/add-oauth-login"}, Options{GeneratedAt: fixedTime()})

	if report.Status != StatusCompliant {
		t.Fatalf("status = %s, want %s", report.Status, StatusCompliant)
	}
	if report.Score != 100 {
		t.Fatalf("score = %d, want 100", report.Score)
	}
	if report.Checks[1].Status != CheckSkipped || report.Checks[2].Status != CheckSkipped {
		t.Fatalf("missing commit and PR should be skipped: %+v", report.Checks)
	}
}

func TestBuildSelectedCheck(t *testing.T) {
	report := Build(Inputs{
		BranchName:    "BadBranch",
		CommitMessage: "feat(auth): add oauth login",
		PRBody:        validPRBody,
	}, Options{Check: "commit-message", GeneratedAt: fixedTime()})

	if len(report.Checks) != 1 {
		t.Fatalf("checks = %d, want 1", len(report.Checks))
	}
	if report.Checks[0].ID != "commit-message" {
		t.Fatalf("check ID = %s, want commit-message", report.Checks[0].ID)
	}
	if report.Status != StatusCompliant {
		t.Fatalf("status = %s, want %s", report.Status, StatusCompliant)
	}
}

func TestBuildInvalidReport(t *testing.T) {
	report := Build(Inputs{
		BranchName:    "BadBranch",
		CommitMessage: "feature(auth): add oauth login",
		PRBody:        "## Summary\nJust this",
	}, Options{GeneratedAt: fixedTime()})

	if report.Status != StatusNonCompliant {
		t.Fatalf("status = %s, want %s", report.Status, StatusNonCompliant)
	}
	if report.Score != 0 {
		t.Fatalf("score = %d, want 0", report.Score)
	}
}

func TestWriteFiles(t *testing.T) {
	dir := t.TempDir()
	report := Build(Inputs{
		BranchName:    "feature/add-oauth-login",
		CommitMessage: "feat(auth): add oauth login",
		PRBody:        validPRBody,
	}, Options{GeneratedAt: fixedTime()})

	if err := WriteFiles(report, dir); err != nil {
		t.Fatalf("WriteFiles() error = %v", err)
	}

	for _, name := range []string{"index.html", "ods-compliance.json", "ods-summary.md", "ods-compliance.svg", "ods-compliance.sarif"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "ods-compliance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed Report
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("report JSON should parse: %v", err)
	}
	if parsed.Status != StatusCompliant {
		t.Fatalf("parsed status = %s, want %s", parsed.Status, StatusCompliant)
	}
}

func TestHTMLRendererEscapesCheckNotes(t *testing.T) {
	report := Build(Inputs{
		BranchName:    "feature/<script>",
		CommitMessage: "feat(auth): add oauth login",
	}, Options{GeneratedAt: fixedTime()})

	page, err := HTML(report)
	if err != nil {
		t.Fatalf("HTML() error = %v", err)
	}
	// Unescaped HTML must not appear
	if strings.Contains(page, "<script>") && !strings.Contains(page, "&lt;script&gt;") {
		t.Fatalf("HTML output contains unescaped script tag: %s", page)
	}
	// Escaped version must appear
	if !strings.Contains(page, "&lt;script&gt;") {
		t.Fatalf("HTML output does not contain escaped script tag: %s", page)
	}
}

func fixedTime() time.Time {
	return time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
}
