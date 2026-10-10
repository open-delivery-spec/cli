package mergeconf

import (
	"regexp"
	"sort"
	"strings"
)

// FileChange is one changed code file with both sides of its diff, for the
// facts that need the removed lines as well as the added ones.
type FileChange struct {
	Path    string
	Added   []string
	Removed []string
	// Deleted is true when the change removes the file.
	Deleted bool
}

// TestWeakening counts the ways a change can make its own tests easier to
// pass: deleting a test file, skipping a test, removing more assertions than
// it adds. These are the patterns maintainers report in agent-authored pull
// requests whose suite went green because the check went away. Each one has a
// legitimate use (dropping a removed feature's tests, quarantining a flaky
// test, consolidating assertions), so they route review, they do not judge.
type TestWeakening struct {
	DeletedTestFiles  []string `json:"deleted_test_files,omitempty"`
	SkipsAdded        int      `json:"skips_added"`
	AssertionsRemoved int      `json:"assertions_removed"`
	AssertionsAdded   int      `json:"assertions_added"`
}

// Weakened reports whether any of the counted patterns is present.
func (w TestWeakening) Weakened() bool {
	return len(w.DeletedTestFiles) > 0 || w.SkipsAdded > 0 || w.AssertionsRemoved > w.AssertionsAdded
}

// skipMarkers match a line that disables a test, across common frameworks.
var skipMarkers = regexp.MustCompile(anyOf(
	`\bt\.Skip(f|Now)?\(`,           // Go
	`@pytest\.mark\.(skip|xfail)\b`, // pytest
	`\bpytest\.skip\(`,
	`@unittest\.skip`, // unittest
	`\bself\.skipTest\(`,
	`\b(it|test|describe|context)\.skip\(`, // Jest, Mocha, Vitest
	`\bx(it|describe|test)\(`,
	`@(Disabled|Ignore)\b`,    // JUnit
	`\[Ignore(\(|\])`,         // NUnit / MSTest
	`#\[ignore\]`,             // Rust
	`\bskip\s*\(?["']`,        // RSpec, Minitest: skip "reason"
	`\bmarkTestSkipped\(`,     // PHPUnit
	`\bXCTSkip(If|Unless)?\(`, // XCTest
))

// assertionMarkers match a line that checks a result, across common
// frameworks. Generous on purpose: a line counts once however many asserts it
// holds, and the fact compares removed with added, so a rename such as
// assert.Equal → require.Equal nets out.
var assertionMarkers = regexp.MustCompile(anyOf(
	`\bassert`,              // assert, assertEqual, assert.Equal, assert_eq!
	`\bexpect\(`,            // Jest, Vitest, Chai, Gomega
	`\bExpect\(`,            // Gomega
	`\brequire\.[A-Z]\w*\(`, // testify
	`\bt\.(Error|Errorf|Fatal|Fatalf|Fail|FailNow)\(`, // Go testing
	`\bXCTAssert`,                  // XCTest
	`\bAssert\.\w+\(`,              // NUnit, MSTest, JUnit 4
	`\bAssertions\.\w+\(`,          // JUnit 5
	`\.should\b`,                   // Chai, RSpec
	`\.to\(|\.not_to\(|\.to_not\(`, // RSpec
	`\bself\.fail\(`,               // unittest
	`\bpytest\.raises\(`,
))

// suppressionMarkers match a line that silences a linter or type checker.
var suppressionMarkers = regexp.MustCompile(anyOf(
	`//\s*nolint\b`,
	`#\s*type:\s*ignore\b`,
	`#\s*noqa\b`,
	`#\s*pylint:\s*disable`,
	`#\s*mypy:\s*ignore`,
	`@ts-(ignore|nocheck|expect-error)\b`,
	`eslint-disable`,
	`@SuppressWarnings\(`,
	`#\[allow\(`,
	`//\s*NOSONAR\b`,
	`#pragma\s+warning\s+disable`,
	`\[SuppressMessage\(`,
	`//\s*@phpstan-ignore`,
	`#\s*rubocop:\s*disable`,
	`//\s*swiftlint:disable`,
))

// anyOf joins alternatives into one regular expression.
func anyOf(alts ...string) string {
	return "(?:" + strings.Join(alts, ")|(?:") + ")"
}

// ComputeTestWeakening derives the test-weakening facts from the changed code
// files. Assertions and skips are counted in test files only; a deleted test
// file is listed, and its lines are not counted again as removed assertions.
func ComputeTestWeakening(changes []FileChange) TestWeakening {
	var w TestWeakening
	for _, c := range changes {
		if !IsTestFile(c.Path) || IsDocFile(c.Path) {
			continue
		}
		if c.Deleted {
			w.DeletedTestFiles = append(w.DeletedTestFiles, c.Path)
			continue
		}
		for _, l := range c.Added {
			if skipMarkers.MatchString(l) {
				w.SkipsAdded++
			}
			if assertionMarkers.MatchString(l) {
				w.AssertionsAdded++
			}
		}
		for _, l := range c.Removed {
			if assertionMarkers.MatchString(l) {
				w.AssertionsRemoved++
			}
		}
	}
	sort.Strings(w.DeletedTestFiles)
	return w
}

// CountSuppressionsAdded counts added lines, in any code file, that silence a
// linter or type checker — the quickest way to make a failing check pass
// without changing what it checks.
func CountSuppressionsAdded(changes []FileChange) int {
	n := 0
	for _, c := range changes {
		for _, l := range c.Added {
			if suppressionMarkers.MatchString(l) {
				n++
			}
		}
	}
	return n
}
