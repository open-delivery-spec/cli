package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestConformance runs the spec repository's conformance suite against this
// reference implementation. Each scenario directory contains input.json (the
// policy input), policy.rego, and expected.json (the expected check output).
//
// Comparison follows the suite's documented semantics: `allowed` and
// `review_tier` are scalar equality; `denials` and `warnings` come from Rego
// partial sets and are compared as *sets*, not ordered lists.
//
// The suite lives in the spec repo, so the test is driven by the
// ODS_CONFORMANCE_DIR env var (CI checks out the spec repo and points it at
// spec/conformance). Without it the test skips, keeping plain `go test ./...`
// hermetic.
func TestConformance(t *testing.T) {
	root := os.Getenv("ODS_CONFORMANCE_DIR")
	if root == "" {
		t.Skip("ODS_CONFORMANCE_DIR not set — skipping spec conformance suite")
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading conformance dir %s: %v", root, err)
	}

	ran := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		scenario := e.Name()
		dir := filepath.Join(root, scenario)
		t.Run(scenario, func(t *testing.T) {
			input := loadJSON[EvalInput](t, filepath.Join(dir, "input.json"))
			want := loadJSON[conformanceExpected](t, filepath.Join(dir, "expected.json"))

			got, err := Evaluate(filepath.Join(dir, "policy.rego"), &input)
			if err != nil {
				t.Fatalf("evaluating policy: %v", err)
			}

			if got.Allowed != want.Allowed {
				t.Errorf("allowed = %v, want %v (denials: %v)", got.Allowed, want.Allowed, got.Denials)
			}
			if got.ReviewTier != want.ReviewTier {
				t.Errorf("review_tier = %q, want %q", got.ReviewTier, want.ReviewTier)
			}
			if !equalAsSets(got.Denials, want.Denials) {
				t.Errorf("denials = %v, want (as set) %v", got.Denials, want.Denials)
			}
			if !equalAsSets(got.Warnings, want.Warnings) {
				t.Errorf("warnings = %v, want (as set) %v", got.Warnings, want.Warnings)
			}
		})
		ran++
	}

	if ran == 0 {
		t.Fatalf("no conformance scenarios found under %s", root)
	}
	t.Logf("conformance: %d scenario(s) evaluated", ran)
}

// conformanceExpected mirrors expected.json (schemas/check-output/v1.json).
type conformanceExpected struct {
	Allowed    bool     `json:"allowed"`
	Denials    []string `json:"denials"`
	Warnings   []string `json:"warnings"`
	ReviewTier string   `json:"review_tier,omitempty"`
}

func loadJSON[T any](t *testing.T, path string) T {
	t.Helper()
	var v T
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return v
}

// equalAsSets compares two string slices ignoring order (and nil vs empty).
func equalAsSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}
