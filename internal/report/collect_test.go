package report

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// gitIn runs git in dir under a fixed identity and fails the test on error. A
// non-empty date pins both the author and the committer date, which keeps the
// --since window and the bucket labels independent of the clock.
func gitIn(t *testing.T, dir, date string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if date != "" {
		cmd.Env = append(cmd.Env, "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// isolateGit clears the variables git uses to choose a repository. Git hooks
// export GIT_DIR and GIT_INDEX_FILE, so a test run from one would otherwise
// build its fixture inside the repository that is being committed to.
func isolateGit(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
		"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX",
	} {
		t.Setenv(key, "") // registers restoring the caller's value when the test ends
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
}

// newFixtureRepo creates an empty repository on branch main and makes it the
// working directory for the test, since Collect reads the repository it runs
// in. It returns the repository path.
func newFixtureRepo(t *testing.T) string {
	t.Helper()
	isolateGit(t)
	dir := t.TempDir()
	gitIn(t, dir, "", "init", "-q", "-b", "main")
	t.Chdir(dir)
	return dir
}

// fixtureCommit is one commit of a fixture history.
type fixtureCommit struct {
	date    string            // author and committer date, ISO 8601 with an explicit offset
	message string            // full message, trailers included
	files   map[string]string // path -> complete new content; no files makes an empty commit
}

// commitAll records the commits in dir, oldest first.
func commitAll(t *testing.T, dir string, commits []fixtureCommit) {
	t.Helper()
	for _, c := range commits {
		for name, content := range c.files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
		args := []string{"commit", "-q", "-m", c.message}
		if len(c.files) == 0 {
			args = append(args, "--allow-empty")
		}
		gitIn(t, dir, "", "add", "-A")
		gitIn(t, dir, c.date, args...)
	}
}

// numbered returns the lines prefix<from> through prefix<to>, each ending in a
// newline, so a fixture file has an exactly known number of lines.
func numbered(prefix string, from, to int) string {
	var b strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "%s%d\n", prefix, i)
	}
	return b.String()
}

const claudeTrailer = "Co-Authored-By: Claude <noreply@anthropic.com>"

// standardHistory is the linear history most Collect tests share, oldest first.
// One commit predates 2026, the rest fall in ISO weeks 28 and 29 of 2026, and
// the churn of each commit is known by construction:
//
//	commit  attribution                    changed lines
//	C0      human, before the window       +4
//	C1      human                          +10
//	C2      Claude                         +5 -2
//	C3      GitHub Copilot                 +4 (a binary file adds no lines)
//	C4      human, with a human co-author  +6
//	C5      Claude Sonnet 4.6              +1 -1
//	C6      human, an empty commit         0
//
// Inside a 2026 window that is 6 commits, 3 of them AI, and 29 changed lines of
// which 13 are AI.
func standardHistory() []fixtureCommit {
	return []fixtureCommit{
		{
			date:    "2025-12-30T12:00:00+0000",
			message: "chore: before the window",
			files:   map[string]string{"old.txt": numbered("o", 1, 4)},
		},
		{
			date:    "2026-07-06T09:00:00+0000", // Monday, ISO week 28
			message: "feat: add main",
			files:   map[string]string{"main.go": numbered("m", 1, 10)},
		},
		{
			date:    "2026-07-07T09:00:00+0000",
			message: "feat: extend main\n\n" + claudeTrailer,
			files:   map[string]string{"main.go": numbered("m", 1, 8) + numbered("a", 1, 5)},
		},
		{
			date:    "2026-07-08T09:00:00+0000",
			message: "feat: add util\n\nCo-Authored-By: copilot-swe-agent[bot] <copilot@github.com>",
			files:   map[string]string{"util.go": numbered("u", 1, 4), "logo.bin": "\x00\x01\x02\x03"},
		},
		{
			date:    "2026-07-14T09:00:00+0000", // Tuesday, ISO week 29
			message: "docs: add guide\n\nCo-authored-by: Dana Dev <dana@example.com>",
			files:   map[string]string{"docs.md": numbered("d", 1, 6)},
		},
		{
			date:    "2026-07-15T09:00:00+0000",
			message: "fix: tweak util\n\nCo-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>",
			files:   map[string]string{"util.go": "u1\nu2x\nu3\nu4\n"},
		},
		{
			date:    "2026-07-16T09:00:00+0000",
			message: "chore: empty commit to retrigger CI",
		},
	}
}

// singleCommit is the smallest history that makes Collect succeed.
func singleCommit() []fixtureCommit {
	return []fixtureCommit{{
		date:    "2026-07-06T09:00:00+0000",
		message: "feat: add main",
		files:   map[string]string{"main.go": numbered("m", 1, 3)},
	}}
}

// TestCollect_standardHistory runs Collect and collectCommits against one real
// repository built from standardHistory. The subtests only read it, so it is
// built once; each subtest guards one behavior.
func TestCollect_standardHistory(t *testing.T) {
	dir := newFixtureRepo(t)
	commitAll(t, dir, standardHistory())
	hashes := strings.Fields(gitIn(t, dir, "", "rev-list", "--reverse", "HEAD")) // hashes[0] is the pre-window commit
	gitIn(t, dir, "", "remote", "add", "origin", "https://github.com/acme/widgets.git")

	// The whole pipeline: git log, trailer attribution (a human co-author is not
	// AI), churn (a binary file adds no lines), the per-tool breakdown that
	// folds model variants into one tool, the chronological weekly trend and the
	// repository name read from the origin remote. The commit that predates the
	// window is left out.
	t.Run("aggregates attribution, churn and trend", func(t *testing.T) {
		r, err := Collect(Options{Since: "2026-01-01"})
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}

		if r.Since != "2026-01-01" {
			t.Errorf("Since = %q, want the requested window", r.Since)
		}
		if r.Repo != "acme/widgets" {
			t.Errorf("Repo = %q, want acme/widgets from the origin remote", r.Repo)
		}
		if r.TotalCommits != 6 || r.AICommits != 3 || r.HumanCommits != 3 {
			t.Errorf("commits = %d total / %d AI / %d human, want 6/3/3", r.TotalCommits, r.AICommits, r.HumanCommits)
		}
		if r.TotalChangedLines != 29 || r.AIChangedLines != 13 {
			t.Errorf("changed lines = %d total / %d AI, want 29/13", r.TotalChangedLines, r.AIChangedLines)
		}
		if r.AICommitShare != 0.5 {
			t.Errorf("AICommitShare = %v, want 0.5", r.AICommitShare)
		}
		if want := 13.0 / 29; math.Abs(r.AILineShare-want) > 1e-9 {
			t.Errorf("AILineShare = %v, want %v", r.AILineShare, want)
		}
		if want := map[string]int{"Claude": 2, "GitHub Copilot": 1}; !maps.Equal(r.ByTool, want) {
			t.Errorf("ByTool = %v, want %v", r.ByTool, want)
		}
		for _, want := range []string{"6 commit(s)", "3 AI-assisted (50%)", "3 human", "45% of changed lines"} {
			if !strings.Contains(r.Summary, want) {
				t.Errorf("summary %q missing %q", r.Summary, want)
			}
		}

		if r.BucketGranularity != "week" {
			t.Errorf("BucketGranularity = %q, want week for a ten-day span", r.BucketGranularity)
		}
		wantBuckets := []TimeBucket{
			{Label: "2026-W28", Start: "2026-07-06", TotalCommits: 3, AICommits: 2, AIShare: 2.0 / 3},
			{Label: "2026-W29", Start: "2026-07-13", TotalCommits: 3, AICommits: 1, AIShare: 1.0 / 3},
		}
		if !slices.Equal(r.Buckets, wantBuckets) {
			t.Errorf("buckets = %+v, want %+v", r.Buckets, wantBuckets)
		}
	})

	// The join between the two git passes (trailers from one, line counts from
	// the other): every commit gets its own tool, date and churn, and git's
	// newest-first order is kept.
	t.Run("collectCommits joins attribution and churn per commit", func(t *testing.T) {
		got, err := collectCommits(Options{Since: "2026-01-01"})
		if err != nil {
			t.Fatalf("collectCommits: %v", err)
		}

		day := func(d int) time.Time { return time.Date(2026, time.July, d, 9, 0, 0, 0, time.UTC) }
		want := []Commit{
			{Hash: hashes[6], Date: day(16)},
			{Hash: hashes[5], Date: day(15), AITool: "Claude", Insertions: 1, Deletions: 1},
			{Hash: hashes[4], Date: day(14), Insertions: 6},
			{Hash: hashes[3], Date: day(8), AITool: "GitHub Copilot", Insertions: 4},
			{Hash: hashes[2], Date: day(7), AITool: "Claude", Insertions: 5, Deletions: 2},
			{Hash: hashes[1], Date: day(6), Insertions: 10},
		}
		if len(got) != len(want) {
			t.Fatalf("got %d commits, want %d: %+v", len(got), len(want), got)
		}
		for i, w := range want {
			g := got[i]
			if g.Hash != w.Hash || g.AITool != w.AITool || g.Insertions != w.Insertions || g.Deletions != w.Deletions || !g.Date.Equal(w.Date) {
				t.Errorf("commit %d = %+v, want %+v", i, g, w)
			}
		}
	})

	// The requested window is handed to git: each window keeps only the commits
	// dated inside it. The cut-offs sit in the gaps of the history, days away
	// from any commit, so the local-midnight reading git gives a bare date
	// cannot move a commit across the boundary.
	t.Run("since window", func(t *testing.T) {
		cases := []struct {
			since        string
			commits, ai  int
			changedLines int
		}{
			{"2025-01-01", 7, 3, 33}, // everything, including the December commit
			{"2026-01-01", 6, 3, 29}, // the December commit falls out
			{"2026-07-10", 3, 1, 8},  // only week 29
		}
		for _, tc := range cases {
			t.Run(tc.since, func(t *testing.T) {
				r, err := Collect(Options{Since: tc.since})
				if err != nil {
					t.Fatalf("Collect: %v", err)
				}
				if r.Since != tc.since {
					t.Errorf("Since = %q, want %q", r.Since, tc.since)
				}
				if r.TotalCommits != tc.commits || r.AICommits != tc.ai || r.TotalChangedLines != tc.changedLines {
					t.Errorf("got %d commits / %d AI / %d lines, want %d/%d/%d",
						r.TotalCommits, r.AICommits, r.TotalChangedLines, tc.commits, tc.ai, tc.changedLines)
				}
			})
		}
	})

	// A window with no commits is a valid, empty report and not an error: a
	// repository that was scanned but had nothing to show still has to appear
	// in an organization roll-up.
	t.Run("empty window is not an error", func(t *testing.T) {
		r, err := Collect(Options{Since: "2030-01-01"})
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if r.TotalCommits != 0 || r.TotalChangedLines != 0 || len(r.ByTool) != 0 {
			t.Errorf("empty window reported activity: %+v", r)
		}
		if r.Summary != "No commits in the selected window" {
			t.Errorf("summary = %q", r.Summary)
		}
		if len(r.Buckets) != 0 || r.BucketGranularity != "" {
			t.Errorf("empty window has a trend: %+v (%q)", r.Buckets, r.BucketGranularity)
		}
	})

	// MaxCommits keeps the newest N commits of the window (not the oldest) in
	// both git passes; zero, or any cap above the window size, means no cap.
	t.Run("max commits keeps the newest", func(t *testing.T) {
		cases := []struct {
			max          int
			commits, ai  int
			changedLines int
		}{
			{0, 6, 3, 29},   // no cap
			{1, 1, 0, 0},    // the empty commit only
			{2, 2, 1, 2},    // plus the Claude fix
			{100, 6, 3, 29}, // more than the window holds
		}
		for _, tc := range cases {
			t.Run(fmt.Sprintf("max=%d", tc.max), func(t *testing.T) {
				r, err := Collect(Options{Since: "2026-01-01", MaxCommits: tc.max})
				if err != nil {
					t.Fatalf("Collect: %v", err)
				}
				if r.TotalCommits != tc.commits || r.AICommits != tc.ai || r.TotalChangedLines != tc.changedLines {
					t.Errorf("got %d commits / %d AI / %d lines, want %d/%d/%d",
						r.TotalCommits, r.AICommits, r.TotalChangedLines, tc.commits, tc.ai, tc.changedLines)
				}
			})
		}
	})
}

// TestCollect_excludesMergeCommits guards the "non-merge commits" contract:
// the work on a branch counts once, through its own commits, and the merge
// commit that brings it in (here carrying an AI trailer that would inflate the
// numbers if it were counted) does not.
func TestCollect_excludesMergeCommits(t *testing.T) {
	dir := newFixtureRepo(t)
	commitAll(t, dir, []fixtureCommit{{
		date: "2026-07-06T09:00:00+0000", message: "feat: base",
		files: map[string]string{"base.txt": numbered("b", 1, 2)},
	}})
	gitIn(t, dir, "", "checkout", "-q", "-b", "feature")
	commitAll(t, dir, []fixtureCommit{{
		date: "2026-07-07T09:00:00+0000", message: "feat: on the branch\n\n" + claudeTrailer,
		files: map[string]string{"feature.txt": numbered("f", 1, 3)},
	}})
	gitIn(t, dir, "", "checkout", "-q", "main")
	commitAll(t, dir, []fixtureCommit{{
		date: "2026-07-08T09:00:00+0000", message: "feat: on main",
		files: map[string]string{"main.txt": numbered("m", 1, 2)},
	}})
	gitIn(t, dir, "2026-07-09T09:00:00+0000", "merge", "-q", "--no-ff", "-m", "Merge feature\n\n"+claudeTrailer, "feature")
	if n := gitIn(t, dir, "", "rev-list", "--merges", "--count", "HEAD"); n != "1" {
		t.Fatalf("fixture has %s merge commits, want 1", n)
	}

	r, err := Collect(Options{Since: "2026-01-01"})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if r.TotalCommits != 3 || r.AICommits != 1 || r.HumanCommits != 2 {
		t.Errorf("commits = %d total / %d AI / %d human, want 3/1/2 (merge commit left out)", r.TotalCommits, r.AICommits, r.HumanCommits)
	}
	if r.TotalChangedLines != 7 || r.AIChangedLines != 3 {
		t.Errorf("changed lines = %d total / %d AI, want 7/3", r.TotalChangedLines, r.AIChangedLines)
	}
	if want := map[string]int{"Claude": 1}; !maps.Equal(r.ByTool, want) {
		t.Errorf("ByTool = %v, want %v", r.ByTool, want)
	}
}

// TestCollect_defaultsToNinetyDayWindow guards the default window: with no
// Since, Collect reports "90 days ago" and applies it. The history has one
// commit years back and one dated a day before the test runs, so the outcome
// holds whatever today's date is.
func TestCollect_defaultsToNinetyDayWindow(t *testing.T) {
	dir := newFixtureRepo(t)
	yesterday := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02T15:04:05+0000")
	commitAll(t, dir, []fixtureCommit{
		{date: "2020-01-01T09:00:00+0000", message: "chore: long ago", files: map[string]string{"old.txt": numbered("o", 1, 2)}},
		{date: yesterday, message: "feat: yesterday", files: map[string]string{"new.txt": numbered("n", 1, 3)}},
	})

	r, err := Collect(Options{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if r.Since != "90 days ago" {
		t.Errorf("Since = %q, want the 90 day default", r.Since)
	}
	if r.TotalCommits != 1 || r.TotalChangedLines != 3 {
		t.Errorf("got %d commits / %d lines, want only yesterday's commit (1 / 3)", r.TotalCommits, r.TotalChangedLines)
	}
}

// TestCollect_repoName guards where the report's repository name comes from:
// an explicit Options.Repo always wins, otherwise the origin remote is used,
// and a repository without an origin gets no name rather than an error.
func TestCollect_repoName(t *testing.T) {
	cases := []struct {
		name   string
		origin string // URL of the origin remote; empty means no remote at all
		repo   string // Options.Repo
		want   string
	}{
		{"derived from an https origin", "https://github.com/acme/widgets.git", "", "acme/widgets"},
		{"derived from an scp-style origin", "git@github.com:acme/widgets.git", "", "acme/widgets"},
		{"explicit name beats the origin", "https://github.com/acme/widgets.git", "other/name", "other/name"},
		{"explicit name without a remote", "", "other/name", "other/name"},
		{"no remote leaves the name empty", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newFixtureRepo(t)
			commitAll(t, dir, singleCommit())
			if tc.origin != "" {
				gitIn(t, dir, "", "remote", "add", "origin", tc.origin)
			}

			r, err := Collect(Options{Since: "2026-01-01", Repo: tc.repo})
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			if r.Repo != tc.want {
				t.Errorf("Repo = %q, want %q", r.Repo, tc.want)
			}
		})
	}
}

// TestCollect_failsOutsideGitRepository guards the failure mode of running the
// report anywhere but in a repository: no report, and an error that says the
// history could not be read and keeps git's exit error for callers.
func TestCollect_failsOutsideGitRepository(t *testing.T) {
	isolateGit(t)
	dir := t.TempDir()
	// A temp root that happens to sit inside a repository would let git find
	// it by walking up; the ceiling stops the search above dir.
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)

	r, err := Collect(Options{Since: "2026-01-01"})
	if err == nil {
		t.Fatal("Collect outside a repository succeeded, want an error")
	}
	if r != nil {
		t.Errorf("report = %+v, want nil alongside the error", r)
	}
	if !strings.Contains(err.Error(), "reading git history") {
		t.Errorf("error = %q, want it to name the history read", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("error %v does not wrap git's exit error", err)
	}
}

// TestCollect_failsWhenGitIsMissing guards the error when the git executable
// cannot be found: it surfaces as exec.ErrNotFound instead of an empty report.
func TestCollect_failsWhenGitIsMissing(t *testing.T) {
	isolateGit(t)
	t.Setenv("PATH", "")

	r, err := Collect(Options{Since: "2026-01-01"})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("error = %v, want exec.ErrNotFound", err)
	}
	if r != nil {
		t.Errorf("report = %+v, want nil alongside the error", r)
	}
}

// TestCollect_failsWhenChurnCannotBeRead guards the second git pass failing on
// its own. Plain git log only needs commit objects, but --numstat has to read
// trees, so a repository with a missing tree object (an interrupted fetch, a
// damaged store) yields attribution but no churn. Collect must report that as
// a churn error rather than return commits with silently zeroed line counts.
func TestCollect_failsWhenChurnCannotBeRead(t *testing.T) {
	dir := newFixtureRepo(t)
	commitAll(t, dir, []fixtureCommit{
		{date: "2026-07-06T09:00:00+0000", message: "feat: one", files: map[string]string{"a.txt": numbered("a", 1, 2)}},
		{date: "2026-07-07T09:00:00+0000", message: "feat: two", files: map[string]string{"b.txt": numbered("b", 1, 2)}},
	})
	tree := gitIn(t, dir, "", "rev-parse", "HEAD^{tree}")
	object := filepath.Join(dir, ".git", "objects", tree[:2], tree[2:])
	if err := os.Chmod(object, 0o644); err != nil { // git writes loose objects read-only
		t.Fatalf("chmod %s: %v", object, err)
	}
	if err := os.Remove(object); err != nil {
		t.Fatalf("remove loose tree object: %v", err)
	}

	r, err := Collect(Options{Since: "2026-01-01"})
	if err == nil {
		t.Fatal("Collect succeeded with an unreadable tree, want an error")
	}
	if r != nil {
		t.Errorf("report = %+v, want nil alongside the error", r)
	}
	if !strings.Contains(err.Error(), "reading git churn") {
		t.Errorf("error = %q, want it to name the churn read", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("error %v does not wrap git's exit error", err)
	}
}

// TestGitOutput guards the thin git wrapper: stdout comes back exactly as git
// wrote it (not trimmed, since the callers parse separators and trailing
// content), and a failing command returns an error with no output.
func TestGitOutput(t *testing.T) {
	newFixtureRepo(t)

	out, err := gitOutput("symbolic-ref", "--short", "HEAD")
	if err != nil {
		t.Fatalf("gitOutput: %v", err)
	}
	if out != "main\n" {
		t.Errorf("output = %q, want %q (untrimmed)", out, "main\n")
	}

	out, err = gitOutput("no-such-subcommand")
	if err == nil {
		t.Error("gitOutput of an unknown subcommand succeeded, want an error")
	}
	if out != "" {
		t.Errorf("output = %q on failure, want empty", out)
	}
}
