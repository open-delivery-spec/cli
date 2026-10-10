package mergeconf

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Claim kinds a pull request description can make that the diff can confirm.
const (
	ClaimTestsAdded   = "tests-added"
	ClaimDocsUpdated  = "docs-updated"
	ClaimFixesIssue   = "fixes-issue"
	maxClaimTextBytes = 200
)

// Claim is one statement the PR description makes about the change, checked
// against the diff. It reads only structured, unambiguous phrasing — ticked
// checkboxes, GitHub closing keywords, short "added tests" sentences — and
// never interprets prose. Verified reports what the diff shows, nothing more:
// a verified tests-added claim means a test file changed, not that the tests
// are any good.
type Claim struct {
	// Claim is the kind: tests-added, docs-updated or fixes-issue.
	Claim string `json:"claim"`
	// Text is the PR-description line that made the claim.
	Text string `json:"text"`
	// Verified is true when the diff contains what the claim implies.
	Verified bool `json:"verified"`
	// Evidence says what the diff showed, for the warning a policy prints.
	Evidence string `json:"evidence"`
}

var (
	checkboxLine = regexp.MustCompile(`^\s*[-*+]\s+\[([ xX])\]\s*(.*)$`)
	htmlComment  = regexp.MustCompile(`(?s)<!--.*?-->`)
	// A negation anywhere in the line withdraws a prose claim: "no tests
	// added", "tests will be added in a follow-up".
	negation = regexp.MustCompile(`(?i)(\bno\b|\bnot\b|n't\b|\bwithout\b|\bnever\b|\bnone\b|\btodo\b|\btbd\b|\bwill\b|follow-?up)`)

	// Ticked checkbox text: a test noun plus a word saying they were added.
	testsNoun = regexp.MustCompile(`(?i)\btests?\b`)
	testsVerb = regexp.MustCompile(`(?i)\b(add|adds|added|update|updates|updated|cover|covers|covered|new|write|wrote|written)\b`)
	// Prose: "added unit tests", "tests were added".
	testsProse = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(add|adds|added|wrote|written|update|updated|new)\s+(unit\s+|integration\s+|e2e\s+|regression\s+|end-to-end\s+)?tests?\b`),
		regexp.MustCompile(`(?i)\btests?\s+(are\s+|were\s+|have\s+been\s+|has\s+been\s+)?(added|updated|written|included)\b`),
	}

	docsNoun  = regexp.MustCompile(`(?i)\b(docs?|documentation|readme|changelog)\b`)
	docsVerb  = regexp.MustCompile(`(?i)\b(add|adds|added|update|updates|updated|write|wrote|written)\b`)
	docsProse = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(added|updated|wrote)\s+(the\s+)?(docs?|documentation|readme|changelog)\b`),
		regexp.MustCompile(`(?i)\b(docs?|documentation|readme|changelog)\s+(is\s+|are\s+|was\s+|were\s+|has\s+been\s+|have\s+been\s+)?(added|updated)\b`),
	}

	// GitHub's closing keywords: "Fixes #12", "closes owner/repo#3", or the
	// issue URL. These are syntax, so no negation check applies.
	closingKeyword = regexp.MustCompile(`(?i)\b(close|closes|closed|fix|fixes|fixed|resolve|resolves|resolved)\s*:?\s+([\w.-]+/[\w.-]+)?#\d+\b|\b(close|closes|closed|fix|fixes|fixed|resolve|resolves|resolved)\s*:?\s+https://github\.com/[\w.-]+/[\w.-]+/issues/\d+`)
)

// CheckClaims reads the claims a PR description makes and checks each against
// the files the change adds or modifies (deleted files excluded: deleting a
// test file does not add a test). One claim per kind, from the first line that
// makes it. Unticked checkboxes, HTML comments, quoted lines and fenced code
// are ignored, so an unfilled template claims nothing.
func CheckClaims(body string, presentFiles []string) []Claim {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	body = htmlComment.ReplaceAllString(body, "")

	found := map[string]string{}
	var order []string
	note := func(kind, line string) {
		if _, ok := found[kind]; ok {
			return
		}
		found[kind] = line
		order = append(order, kind)
	}

	inFence := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || line == "" || strings.HasPrefix(line, ">") {
			continue
		}

		if closingKeyword.MatchString(line) {
			note(ClaimFixesIssue, line)
		}

		if m := checkboxLine.FindStringSubmatch(line); m != nil {
			if m[1] == " " {
				continue // unticked: the template asked, the author did not claim
			}
			text := m[2]
			if testsNoun.MatchString(text) && testsVerb.MatchString(text) {
				note(ClaimTestsAdded, line)
			}
			if docsNoun.MatchString(text) && docsVerb.MatchString(text) {
				note(ClaimDocsUpdated, line)
			}
			continue
		}

		if negation.MatchString(line) {
			continue
		}
		for _, re := range testsProse {
			if re.MatchString(line) {
				note(ClaimTestsAdded, line)
				break
			}
		}
		for _, re := range docsProse {
			if re.MatchString(line) {
				note(ClaimDocsUpdated, line)
				break
			}
		}
	}

	var tests, docs, other []string
	for _, f := range presentFiles {
		// Documentation first: a README under a test/ or spec/ directory
		// documents, it does not test.
		switch {
		case IsDocFile(f):
			docs = append(docs, f)
			other = append(other, f)
		case IsTestFile(f):
			tests = append(tests, f)
		default:
			other = append(other, f)
		}
	}

	claims := make([]Claim, 0, len(order))
	for _, kind := range order {
		c := Claim{Claim: kind, Text: truncate(found[kind], maxClaimTextBytes)}
		switch kind {
		case ClaimTestsAdded:
			c.Verified = len(tests) > 0
			c.Evidence = filesEvidence(tests, "test file", "no test file was added or modified")
		case ClaimDocsUpdated:
			c.Verified = len(docs) > 0
			c.Evidence = filesEvidence(docs, "documentation file", "no documentation file was added or modified")
		case ClaimFixesIssue:
			// A fix changes something other than tests. A "fix" whose only
			// change is to a test is the no-op fix pattern: the failure goes
			// away because the check did, not because the code changed.
			c.Verified = len(other) > 0
			if c.Verified {
				c.Evidence = filesEvidence(other, "non-test file", "")
			} else if len(tests) > 0 {
				c.Evidence = "only test files changed"
			} else {
				c.Evidence = "no file was added or modified"
			}
		}
		claims = append(claims, c)
	}
	return claims
}

// IsDocFile reports whether a path is documentation: a Markdown, reST or
// AsciiDoc file, anything under a docs/ or doc/ directory, or a README /
// CHANGELOG at any level. Plain .txt is not enough on its own:
// requirements.txt is a dependency manifest.
func IsDocFile(p string) bool {
	lp := strings.ToLower(strings.TrimSpace(p))
	if lp == "" {
		return false
	}
	switch path.Ext(lp) {
	case ".md", ".mdx", ".markdown", ".rst", ".adoc", ".asciidoc":
		return true
	}
	base := path.Base(lp)
	if strings.HasPrefix(base, "readme") || strings.HasPrefix(base, "changelog") {
		return true
	}
	for _, seg := range strings.Split(path.Dir(lp), "/") {
		if seg == "docs" || seg == "doc" {
			return true
		}
	}
	return false
}

func filesEvidence(files []string, noun, none string) string {
	switch len(files) {
	case 0:
		return none
	case 1:
		return fmt.Sprintf("1 %s changed: %s", noun, files[0])
	default:
		return fmt.Sprintf("%d %ss changed, e.g. %s", len(files), noun, files[0])
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Cut on a rune boundary.
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}
