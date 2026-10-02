package cmd

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// splitCmd returns a bare cobra.Command whose stdout and stderr are captured
// in separate buffers, for commands that print JSON and warn on stderr.
func splitCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	c := &cobra.Command{}
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	c.SetOut(out)
	c.SetErr(errOut)
	return c, out, errOut
}

// gitRun runs git in dir with a fixed identity and fails the test on error.
// TestMain already isolates git from the developer's global config.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo creates a fixture repository on branch main with one docs commit,
// makes it the working directory, and returns its path.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	mustWrite(t, dir+"/README.md", "# fixture\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "chore: init")
	t.Chdir(dir)
	return dir
}

// commitAll stages everything in dir and commits it with msg.
func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", msg)
}

// clearPipelineEnv blanks every environment variable the commands read, so a
// test sees the same inputs locally and in CI, where GITHUB_* is set.
func clearPipelineEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"ODS_DIFF_BASE", "ODS_BRANCH", "ODS_BRANCH_NAME", "ODS_PR_BODY",
		"ODS_HEAD_SHA", "ODS_PIPELINE_INTEGRITY", "ODS_DEBUG",
		"GITHUB_HEAD_REF", "GITHUB_REPOSITORY", "GITHUB_REF",
		"GITHUB_SERVER_URL", "GITHUB_RUN_ID",
	} {
		t.Setenv(name, "")
	}
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what
// it printed, for code that writes to os.Stdout rather than the command.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	fn()
	w.Close()
	return string(<-done)
}
