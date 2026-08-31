package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// The spec's conformance suite is also evaluated in internal/policy, which
// exercises the Rego semantics directly. This file covers what that test cannot
// see: the *CLI contract* around the same scenarios — that `ods check --input`
// emits check-output/v1 on stdout with the fields at the top level, and that a
// denial exits non-zero. The exit code is the mechanism that actually blocks a
// merge in CI, so it needs a test that runs the real binary.

// checkOutput mirrors schemas/check-output/v1.json.
type checkOutput struct {
	Allowed    bool     `json:"allowed"`
	Denials    []string `json:"denials"`
	Warnings   []string `json:"warnings"`
	ReviewTier string   `json:"review_tier"`
}

// conformanceRoot returns the spec's conformance directory, or skips. CI checks
// the spec repo out and points ODS_CONFORMANCE_DIR at it; locally, a sibling
// checkout of the spec repo is picked up automatically.
func conformanceRoot(t *testing.T) string {
	t.Helper()
	if root := os.Getenv("ODS_CONFORMANCE_DIR"); root != "" {
		if _, err := os.Stat(root); err != nil {
			t.Fatalf("ODS_CONFORMANCE_DIR=%s is not readable: %v", root, err)
		}
		return root
	}
	for _, candidate := range []string{
		"../../../spec/spec/conformance",
		"../../spec/spec/conformance",
	} {
		if _, err := os.Stat(candidate); err == nil {
			if abs, err := filepath.Abs(candidate); err == nil {
				return abs
			}
		}
	}
	t.Skip("ODS_CONFORMANCE_DIR not set and no sibling spec checkout found")
	return ""
}

// TestConformanceThroughCLI runs every spec scenario through the real binary and
// asserts the two things only the CLI can get wrong: the shape of --json output,
// and the exit code that gates the merge.
func TestConformanceThroughCLI(t *testing.T) {
	root := conformanceRoot(t)

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading conformance dir %s: %v", root, err)
	}

	ran := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(dir, "input.json")); err != nil {
			continue
		}
		ran++

		t.Run(entry.Name(), func(t *testing.T) {
			var want checkOutput
			raw, err := os.ReadFile(filepath.Join(dir, "expected.json"))
			if err != nil {
				t.Fatalf("reading expected.json: %v", err)
			}
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatalf("parsing expected.json: %v", err)
			}

			stdout, stderr, exit := runODSStreams(t, ".", "check",
				"--input", filepath.Join(dir, "input.json"),
				"--policy", filepath.Join(dir, "policy.rego"),
				"--json")

			var got checkOutput
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("stdout is not check-output JSON: %v\nstdout: %s\nstderr: %s",
					err, stdout, stderr)
			}

			if got.Allowed != want.Allowed {
				t.Errorf("allowed = %v, want %v (denials: %v)", got.Allowed, want.Allowed, got.Denials)
			}
			// An omitted review_tier reads as "standard" on both sides.
			if tierOrStandard(got.ReviewTier) != tierOrStandard(want.ReviewTier) {
				t.Errorf("review_tier = %q, want %q", got.ReviewTier, want.ReviewTier)
			}
			assertSameSet(t, "denials", got.Denials, want.Denials)
			assertSameSet(t, "warnings", got.Warnings, want.Warnings)

			// The gate blocks a merge by exiting non-zero, not by what it prints.
			if wantBlocked := !want.Allowed; wantBlocked != (exit != 0) {
				t.Errorf("exit code = %d, want %s for allowed=%v",
					exit, map[bool]string{true: "non-zero", false: "zero"}[wantBlocked], want.Allowed)
			}
		})
	}

	if ran == 0 {
		t.Fatalf("no conformance scenarios found under %s", root)
	}
	t.Logf("conformance: %d scenario(s) run through the CLI", ran)
}

func tierOrStandard(tier string) string {
	if tier == "" {
		return "standard"
	}
	return tier
}

func assertSameSet(t *testing.T, field string, got, want []string) {
	t.Helper()
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if len(g) != len(w) {
		t.Errorf("%s: got %d, want %d\n  got:  %v\n  want: %v", field, len(g), len(w), g, w)
		return
	}
	for i := range g {
		if g[i] != w[i] {
			t.Errorf("%s differ:\n  got:  %v\n  want: %v", field, g, w)
			return
		}
	}
}

// TestCheckInput_NeedsNoRepository proves the point of --input: a scenario runs
// with no git repository and no working-tree state to reproduce.
func TestCheckInput_NeedsNoRepository(t *testing.T) {
	dir := t.TempDir() // deliberately not a git repo
	writeFile(t, dir, "input.json", `{
  "ai_generated": true,
  "ai_confidence": 0.9,
  "technical_debt_delta": 0.3,
  "test_coverage": 0.82,
  "issues": [],
  "ai_files": [],
  "changed_files": ["internal/auth/session.go"]
}`)
	writeFile(t, dir, "policy.rego", `package ods.policy

default allow := true
default review_tier := "standard"

review_tier := "auto" {
    input.technical_debt_delta <= 1.0
}
`)

	stdout, stderr, exit := runODSStreams(t, dir, "check",
		"--input", filepath.Join(dir, "input.json"),
		"--policy", filepath.Join(dir, "policy.rego"), "--json")

	var got checkOutput
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not check-output JSON: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !got.Allowed || exit != 0 {
		t.Errorf("allowed=%v exit=%d, want an allowed change outside a repository", got.Allowed, exit)
	}
	if got.ReviewTier != "auto" {
		t.Errorf("review_tier = %q, want auto", got.ReviewTier)
	}
}
