package profiles_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/open-delivery-spec/cli/internal/policy"
	"github.com/open-delivery-spec/cli/internal/profiles"
)

// eval writes a profile to a temp file and evaluates it against input.
func eval(t *testing.T, profileName string, input *policy.EvalInput) *policy.EvalResult {
	t.Helper()
	p, err := profiles.Get(profileName)
	if err != nil {
		t.Fatalf("Get(%q): %v", profileName, err)
	}
	path := filepath.Join(t.TempDir(), "policy.rego")
	if err := os.WriteFile(path, []byte(p.Policy), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := policy.Evaluate(path, input)
	if err != nil {
		t.Fatalf("profile %q is not valid/evaluable Rego: %v", profileName, err)
	}
	return res
}

func TestAllProfilesAreValidRego(t *testing.T) {
	for _, p := range profiles.All() {
		// A trivial input must at least evaluate without error.
		eval(t, p.Name, &policy.EvalInput{})
	}
}

func TestNamesDefaultFirst(t *testing.T) {
	names := profiles.Names()
	if len(names) == 0 || names[0] != profiles.Default {
		t.Fatalf("Names() = %v, want default %q first", names, profiles.Default)
	}
}

func TestGetUnknownProfile(t *testing.T) {
	if _, err := profiles.Get("nope"); err == nil {
		t.Error("expected error for unknown profile")
	}
}

var (
	critical = &policy.EvalInput{Issues: []policy.EvalIssue{{Rule: "r", Severity: "critical", File: "a", Line: 1}}}
	high     = &policy.EvalInput{AIGenerated: true, Issues: []policy.EvalIssue{{Rule: "r", Severity: "high", File: "a", Line: 1}}}
	lowCovAI = &policy.EvalInput{AIGenerated: true, TestCoverage: 0.2}
	clean    = &policy.EvalInput{TechnicalDebtDelta: 0.3, TestCoverage: 0.9}
)

func TestOdsWayBlocksCriticalNotHigh(t *testing.T) {
	if eval(t, "ods-way", critical).Allowed {
		t.Error("ods-way must block a critical issue")
	}
	if !eval(t, "ods-way", high).Allowed {
		t.Error("ods-way must NOT block a high issue (only warns/routes)")
	}
	if got := eval(t, "ods-way", high).ReviewTier; got != "elevated" {
		t.Errorf("ods-way AI+high tier = %q, want elevated", got)
	}
}

func TestStrictBlocksHighAndLowCoverage(t *testing.T) {
	if eval(t, "strict", high).Allowed {
		t.Error("strict must block a high issue")
	}
	if eval(t, "strict", lowCovAI).Allowed {
		t.Error("strict must block AI code below the coverage floor")
	}
	if !eval(t, "strict", clean).Allowed {
		t.Error("strict must allow a clean, well-covered change")
	}
	if got := eval(t, "strict", clean).ReviewTier; got != "auto" {
		t.Errorf("strict clean+covered tier = %q, want auto", got)
	}
}

func TestAdvisoryNeverBlocks(t *testing.T) {
	// Never blocks, for any input.
	for _, in := range []*policy.EvalInput{critical, high, lowCovAI} {
		if res := eval(t, "advisory", in); !res.Allowed {
			t.Errorf("advisory must never block, but denied: %v", res.Denials)
		}
	}
	// Still surfaces findings as warnings.
	for _, in := range []*policy.EvalInput{critical, high} {
		if len(eval(t, "advisory", in).Warnings) == 0 {
			t.Error("advisory should surface a warning when there are findings")
		}
	}
	// Routing still works under advisory.
	if got := eval(t, "advisory", high).ReviewTier; got != "elevated" {
		t.Errorf("advisory AI+high tier = %q, want elevated", got)
	}
}
