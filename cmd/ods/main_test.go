package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// TestMainRunsTheCLI: main hands os.Args to the command tree and returns
// normally when the command succeeds, here printing the build version.
func TestMainRunsTheCLI(t *testing.T) {
	oldArgs, oldStdout := os.Args, os.Stdout
	t.Cleanup(func() { os.Args, os.Stdout = oldArgs, oldStdout })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Args = []string{"ods", "--version"}
	os.Stdout = w

	main()

	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "ods dev (commit unknown, built unknown)") {
		t.Errorf("ods --version printed %q", out)
	}
}
