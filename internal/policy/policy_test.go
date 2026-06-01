package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProfilePresets(t *testing.T) {
	presets := profilePresets()

	for _, name := range []string{ProfileOSS, ProfileEnterprise, ProfileRegulated} {
		p, ok := presets[name]
		if !ok {
			t.Fatalf("missing profile: %s", name)
		}
		if p.Profile != name {
			t.Fatalf("profile %s name mismatch: %s", name, p.Profile)
		}
		if len(p.Branch.AllowedTypes) == 0 {
			t.Fatalf("profile %s has no branch types", name)
		}
		if len(p.PR.RequiredSections) == 0 {
			t.Fatalf("profile %s has no PR sections", name)
		}
		if len(p.Commit.AllowedTypes) == 0 {
			t.Fatalf("profile %s has no commit types", name)
		}
	}

	// OSS: AI disclosure is optional
	if presets[ProfileOSS].AIDisclosure.Required {
		t.Fatal("OSS profile should not require AI disclosure")
	}
	if presets[ProfileOSS].PR.RequiredSections[2] == "## AI Disclosure" {
		t.Fatal("OSS profile should not require AI Disclosure section")
	}

	// Enterprise: AI disclosure is required
	if !presets[ProfileEnterprise].AIDisclosure.Required {
		t.Fatal("Enterprise profile should require AI disclosure")
	}
	// Scope is optional per Conventional Commits spec
	if presets[ProfileEnterprise].Commit.RequireScope {
		t.Fatal("Enterprise profile should not require commit scope (scope is optional per spec)")
	}

	// Regulated: most strict
	if !presets[ProfileRegulated].Branch.RequireTicket {
		t.Fatal("Regulated profile should require tickets")
	}
	hasRiskAssessment := false
	for _, s := range presets[ProfileRegulated].PR.RequiredSections {
		if s == "## Risk Assessment" {
			hasRiskAssessment = true
		}
	}
	if !hasRiskAssessment {
		t.Fatal("Regulated profile should require Risk Assessment")
	}
}

func TestLoadPolicyFromFile(t *testing.T) {
	policyYAML := `
profile: regulated
branch:
  allowed_types:
    - feature
    - bugfix
    - hotfix
pr:
  min_changes: 3
ai_disclosure:
  required: true
  strict_tool_name: true
severity:
  commit_scope: error
`
	dir := t.TempDir()
	path := filepath.Join(dir, ".ods.yaml")
	if err := os.WriteFile(path, []byte(policyYAML), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadPolicyFromFile(path)
	if err != nil {
		t.Fatalf("LoadPolicyFromFile: %v", err)
	}

	if p.Profile != ProfileRegulated {
		t.Fatalf("profile = %s, want %s", p.Profile, ProfileRegulated)
	}
	if len(p.Branch.AllowedTypes) != 3 {
		t.Fatalf("branch allowed_types = %d, want 3", len(p.Branch.AllowedTypes))
	}
	if p.PR.MinChanges != 3 {
		t.Fatalf("pr min_changes = %d, want 3", p.PR.MinChanges)
	}
	if !p.AIDisclosure.Required {
		t.Fatal("ai_disclosure.required should be true")
	}
	if s := p.GetSeverity("commit_scope"); s != SeverityError {
		t.Fatalf("commit_scope severity = %s, want error", s)
	}

	// Inherited from regulated preset: ticket required, max description
	if !p.Branch.RequireTicket {
		t.Fatal("regulated profile should require tickets")
	}
}

func TestLoadPolicyFromFileOSS(t *testing.T) {
	policyYAML := `profile: oss`
	dir := t.TempDir()
	path := filepath.Join(dir, ".ods.yaml")
	if err := os.WriteFile(path, []byte(policyYAML), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadPolicyFromFile(path)
	if err != nil {
		t.Fatalf("LoadPolicyFromFile: %v", err)
	}

	if p.Profile != ProfileOSS {
		t.Fatalf("profile = %s, want %s", p.Profile, ProfileOSS)
	}
	if p.AIDisclosure.Required {
		t.Fatal("OSS AI disclosure should not be required")
	}
	if p.Commit.RequireScope {
		t.Fatal("OSS commit scope should not be required")
	}
}

func TestGetSeverity(t *testing.T) {
	p := &Policy{
		SeverityMap: map[string]Severity{
			"branch_type": SeverityError,
			"pr_ai":       SeverityWarning,
		},
	}

	if s := p.GetSeverity("branch_type"); s != SeverityError {
		t.Fatalf("branch_type = %s, want error", s)
	}
	if s := p.GetSeverity("pr_ai"); s != SeverityWarning {
		t.Fatalf("pr_ai = %s, want warning", s)
	}
	if s := p.GetSeverity("pr_ai_disclosure"); s != SeverityWarning {
		t.Fatalf("unconfigured ai rule should default to warning: got %s", s)
	}
	if s := p.GetSeverity("branch_format"); s != SeverityError {
		t.Fatalf("unconfigured non-ai rule should default to error: got %s", s)
	}
}

func TestMergePolicies(t *testing.T) {
	dst := &Policy{
		Profile: ProfileOSS,
		Branch: BranchConfig{
			AllowedTypes: []string{"feature"},
		},
	}
	src := &Policy{
		Profile: ProfileEnterprise,
		Branch: BranchConfig{
			AllowedTypes:  []string{"feature", "bugfix", "hotfix"},
			RequireTicket: true,
		},
	}

	mergePolicies(dst, src)

	if dst.Profile != ProfileEnterprise {
		t.Fatalf("profile = %s, want %s", dst.Profile, ProfileEnterprise)
	}
	if len(dst.Branch.AllowedTypes) != 3 {
		t.Fatalf("branch types = %d, want 3", len(dst.Branch.AllowedTypes))
	}
	if !dst.Branch.RequireTicket {
		t.Fatal("require_ticket should be true after merge")
	}
}

func TestDiscoverPolicyFile(t *testing.T) {
	// No .ods.yaml in test dir
	path := DiscoverPolicyFile()
	if path != "" {
		t.Logf("found policy file: %s (unexpected in test, but may exist in repo)", path)
	}
}
