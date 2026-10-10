package mergeconf

import (
	"strings"
	"testing"
)

// odsTemplate is examples/ods-pr-template.md from the spec, filled in the way
// an author leaves it when they tick nothing.
const odsTemplate = `## Summary
<!-- Brief description of what this PR does and why. -->

## AI Disclosure
- [ ] This PR contains AI-generated code
- **AI Tool:** <!-- e.g. GitHub Copilot -->

## Testing
- [ ] Unit tests added/updated
- [ ] Integration tests added/updated
- [ ] Manual testing performed

## Checklist
- [ ] Tests cover AI-generated logic
- [ ] Documentation updated (if applicable)
`

func TestCheckClaims(t *testing.T) {
	src := []string{"internal/svc.go"}
	withTest := []string{"internal/svc.go", "internal/svc_test.go"}
	withDocs := []string{"internal/svc.go", "docs/guide.md"}
	onlyTest := []string{"internal/svc_test.go"}

	cases := []struct {
		name  string
		body  string
		files []string
		want  map[string]bool // claim kind -> verified; absent kinds must not appear
	}{
		{"empty body claims nothing", "", src, nil},
		{"unticked template claims nothing", odsTemplate, src, nil},
		{"ticked tests box without a test file", strings.Replace(odsTemplate, "- [ ] Unit tests added/updated", "- [x] Unit tests added/updated", 1), src,
			map[string]bool{ClaimTestsAdded: false}},
		{"ticked tests box with a test file", strings.Replace(odsTemplate, "- [ ] Unit tests added/updated", "- [X] Unit tests added/updated", 1), withTest,
			map[string]bool{ClaimTestsAdded: true}},
		{"ticked coverage box counts as a tests claim", "- [x] Tests cover AI-generated logic", src,
			map[string]bool{ClaimTestsAdded: false}},
		{"manual testing is not a tests claim", "- [x] Manual testing performed", src, nil},
		{"ticked docs box", "- [x] Documentation updated (if applicable)", withDocs,
			map[string]bool{ClaimDocsUpdated: true}},
		{"ticked docs box without docs", "- [x] Documentation updated", src,
			map[string]bool{ClaimDocsUpdated: false}},
		{"prose tests claim", "## Changes\n- Added unit tests for the parser", src,
			map[string]bool{ClaimTestsAdded: false}},
		{"passive prose tests claim", "Tests were added for the retry path.", withTest,
			map[string]bool{ClaimTestsAdded: true}},
		{"negated prose claims nothing", "No tests added: covered by the existing suite.\nTests will be added in a follow-up.", src, nil},
		{"closing keyword with a source change", "Fixes #42", src,
			map[string]bool{ClaimFixesIssue: true}},
		{"closing keyword with only a test change", "Closes owner/repo#7", onlyTest,
			map[string]bool{ClaimFixesIssue: false}},
		{"closing keyword by URL", "Resolves https://github.com/o/r/issues/9", src,
			map[string]bool{ClaimFixesIssue: true}},
		{"a bare issue reference is not a fix claim", "Related to #42", src, nil},
		{"a docs-only fix is a fix", "Fixes #3: typo in the guide", []string{"docs/guide.md"},
			map[string]bool{ClaimFixesIssue: true}},
		{"quoted and fenced text is ignored", "> Fixes #1\n```\n- [x] Unit tests added\nFixes #2\n```", src, nil},
		{"commented-out claims are ignored", "<!-- - [x] Unit tests added -->\n<!--\nFixes #5\n-->", src, nil},
		{"one claim per kind, first line wins", "Fixes #1\nFixes #2\n- [x] Unit tests added\n- [x] Integration tests added", withTest,
			map[string]bool{ClaimFixesIssue: true, ClaimTestsAdded: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckClaims(tc.body, tc.files)
			if len(got) != len(tc.want) {
				t.Fatalf("CheckClaims = %+v, want kinds %v", got, tc.want)
			}
			for _, c := range got {
				want, ok := tc.want[c.Claim]
				if !ok {
					t.Errorf("unexpected claim %+v", c)
					continue
				}
				if c.Verified != want {
					t.Errorf("%s verified = %t, want %t (evidence %q)", c.Claim, c.Verified, want, c.Evidence)
				}
				if c.Text == "" || c.Evidence == "" {
					t.Errorf("%s: text and evidence must be set, got %+v", c.Claim, c)
				}
			}
		})
	}
}

func TestCheckClaims_evidenceAndOrder(t *testing.T) {
	got := CheckClaims("- [x] Unit tests added\nFixes #8", []string{"a_test.go", "b_test.go"})
	if len(got) != 2 || got[0].Claim != ClaimTestsAdded || got[1].Claim != ClaimFixesIssue {
		t.Fatalf("claims = %+v, want tests-added then fixes-issue in body order", got)
	}
	if got[0].Evidence != "2 test files changed, e.g. a_test.go" {
		t.Errorf("tests evidence = %q", got[0].Evidence)
	}
	if got[1].Verified || got[1].Evidence != "only test files changed" {
		t.Errorf("fix claim = %+v, want unverified with only test files changed", got[1])
	}
	if c := CheckClaims("Fixes #8", nil); len(c) != 1 || c[0].Evidence != "no file was added or modified" {
		t.Errorf("fix with no files = %+v", c)
	}
}

func TestCheckClaims_truncatesLongLines(t *testing.T) {
	long := "Fixes #1 " + strings.Repeat("é", 300)
	got := CheckClaims(long, []string{"a.go"})
	if len(got) != 1 {
		t.Fatalf("claims = %+v", got)
	}
	if len(got[0].Text) > maxClaimTextBytes+len("…") || !strings.HasSuffix(got[0].Text, "…") {
		t.Errorf("text not truncated: %d bytes", len(got[0].Text))
	}
	if !strings.HasPrefix(got[0].Text, "Fixes #1") {
		t.Errorf("text = %q", got[0].Text)
	}
}

func TestIsDocFile(t *testing.T) {
	cases := map[string]bool{
		"README.md":              true,
		"docs/guide.md":          true,
		"docs/conf.py":           true,
		"spec/docs/schemas.md":   true,
		"CHANGELOG":              true,
		"pkg/doc/usage.txt":      true,
		"requirements.txt":       false,
		"internal/svc.go":        false,
		"tests/README.rst":       true,
		"guide.adoc":             true,
		"":                       false,
		"internal/document.go":   false,
		"internal/docs_test.go":  false,
		"site/content/about.mdx": true,
	}
	for p, want := range cases {
		if got := IsDocFile(p); got != want {
			t.Errorf("IsDocFile(%q) = %v, want %v", p, got, want)
		}
	}
}
