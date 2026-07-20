package rules

import "testing"

func TestAll(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("All() returned no rules")
	}
	for _, r := range all {
		if r.ID == "" {
			t.Error("rule with empty ID")
		}
		if r.Name == "" || r.Description == "" || r.Suggestion == "" {
			t.Errorf("rule %s has incomplete metadata: %+v", r.ID, r)
		}
		switch r.DefaultSeverity {
		case "critical", "high", "medium", "low", "info":
		default:
			t.Errorf("rule %s has invalid severity %q", r.ID, r.DefaultSeverity)
		}
	}
}

func TestAll_ReturnsCopy(t *testing.T) {
	a := All()
	if len(a) == 0 {
		t.Fatal("empty registry")
	}
	a[0].ID = "mutated"
	if All()[0].ID == "mutated" {
		t.Error("All() exposed the underlying slice — mutation leaked back into the registry")
	}
}

func TestGet(t *testing.T) {
	r, ok := Get(OverCommenting)
	if !ok {
		t.Fatalf("Get(%q) not found", OverCommenting)
	}
	if r.ID != OverCommenting {
		t.Errorf("Get returned wrong rule: %s", r.ID)
	}
	if _, ok := Get("does-not-exist"); ok {
		t.Error("Get returned ok for unknown rule")
	}
}

func TestIDs(t *testing.T) {
	ids := IDs()
	if len(ids) != len(All()) {
		t.Fatalf("IDs() len = %d, want %d", len(ids), len(All()))
	}
}

// TestConstantsAreRegistered guards against drift: every exported rule-ID
// constant must have a corresponding registry entry, and vice versa.
func TestConstantsAreRegistered(t *testing.T) {
	consts := []string{
		RedundantErrorHandling,
		OverCommenting,
		UnsafeDeserialization,
		InconsistentPattern,
		HallucinatedAPI,
	}
	for _, id := range consts {
		if _, ok := Get(id); !ok {
			t.Errorf("constant %q has no registry entry", id)
		}
	}
	if len(consts) != len(All()) {
		t.Errorf("constant count (%d) != registry size (%d) — a rule was added to one but not the other",
			len(consts), len(All()))
	}
}
