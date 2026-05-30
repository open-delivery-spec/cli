// Package policy provides enterprise-grade ODS policy configuration.
// It supports configurable branch types, PR sections, AI disclosure rules,
// severity levels, and profile presets (oss, enterprise, regulated).
package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Severity controls how a rule violation is treated.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Profile presets for common organizational needs.
const (
	ProfileOSS        = "oss"
	ProfileEnterprise = "enterprise"
	ProfileRegulated  = "regulated"
)

// AIDisclosure configures AI disclosure requirements.
type AIDisclosure struct {
	// Required controls whether AI disclosure is mandatory or optional.
	Required bool `mapstructure:"required" json:"required"`

	// StrictToolName requires the AI tool name to be specified when AI code is present.
	StrictToolName bool `mapstructure:"strict_tool_name" json:"strict_tool_name"`

	// RequireHumanReview requires human review documentation when AI code is present.
	RequireHumanReview bool `mapstructure:"require_human_review" json:"require_human_review"`

	// AIBranchNaming controls how AI-prefixed branches are treated.
	// Possible values: "warning", "error", "allow", "deny".
	AIBranchNaming string `mapstructure:"ai_branch_naming" json:"ai_branch_naming"`
}

// BranchConfig controls branch naming validation rules.
type BranchConfig struct {
	// AllowedTypes lists the permitted branch type prefixes.
	AllowedTypes []string `mapstructure:"allowed_types" json:"allowed_types"`

	// RequireTicket, when true, requires a ticket ID in the branch name.
	RequireTicket bool `mapstructure:"require_ticket" json:"require_ticket"`

	// MaxDescriptionLength sets the maximum length for the description portion.
	MaxDescriptionLength int `mapstructure:"max_description_length" json:"max_description_length"`
}

// PRConfig controls pull request description validation rules.
type PRConfig struct {
	// RequiredSections lists the sections that must be present in a PR body.
	RequiredSections []string `mapstructure:"required_sections" json:"required_sections"`

	// MinChanges is the minimum number of change entries required.
	MinChanges int `mapstructure:"min_changes" json:"min_changes"`
}

// CommitConfig controls commit message validation rules.
type CommitConfig struct {
	// AllowedTypes lists permitted conventional commit types.
	AllowedTypes []string `mapstructure:"allowed_types" json:"allowed_types"`

	// RequireScope, when true, requires a scope in parentheses.
	RequireScope bool `mapstructure:"require_scope" json:"require_scope"`

	// MaxSubjectLength sets the maximum length for the commit subject line.
	MaxSubjectLength int `mapstructure:"max_subject_length" json:"max_subject_length"`
}

// Policy defines the full enterprise ODS policy configuration.
type Policy struct {
	// Profile selects a preset: oss, enterprise, or regulated.
	Profile string `mapstructure:"profile" json:"profile"`

	// SeverityMap maps rule IDs to severity overrides.
	// Keys: "branch_type", "branch_format", "pr_sections", "pr_ai_disclosure",
	// "pr_ai_tool", "commit_type", "commit_scope", "commit_ai".
	SeverityMap map[string]Severity `mapstructure:"severity" json:"severity"`

	// Branch controls branch naming rules.
	Branch BranchConfig `mapstructure:"branch" json:"branch"`

	// PR controls pull request description rules.
	PR PRConfig `mapstructure:"pr" json:"pr"`

	// Commit controls commit message rules.
	Commit CommitConfig `mapstructure:"commit" json:"commit"`

	// AIDisclosure controls AI disclosure requirements.
	AIDisclosure AIDisclosure `mapstructure:"ai_disclosure" json:"ai_disclosure"`
}

// profilePresets returns hardcoded defaults for each profile.
func profilePresets() map[string]Policy {
	return map[string]Policy{
		ProfileOSS: {
			Profile: ProfileOSS,
			Branch: BranchConfig{
				AllowedTypes:         []string{"feature", "feat", "bugfix", "fix", "hotfix", "release", "chore"},
				RequireTicket:        false,
				MaxDescriptionLength: 100,
			},
			PR: PRConfig{
				RequiredSections: []string{"## Summary", "## Type", "## Changes", "## Testing", "## Checklist"},
				MinChanges:       1,
			},
			Commit: CommitConfig{
				AllowedTypes:    []string{"feat", "fix", "docs", "style", "refactor", "perf", "test", "build", "ci", "chore", "revert"},
				RequireScope:    false,
				MaxSubjectLength: 100,
			},
			AIDisclosure: AIDisclosure{
				Required:            false,
				StrictToolName:      false,
				RequireHumanReview:  false,
				AIBranchNaming:      "warning",
			},
			SeverityMap: map[string]Severity{
				"branch_type":     SeverityError,
				"branch_format":   SeverityError,
				"pr_sections":     SeverityError,
				"pr_ai_disclosure": SeverityWarning,
				"pr_ai_tool":      SeverityWarning,
				"commit_type":     SeverityError,
				"commit_ai":       SeverityWarning,
			},
		},
		ProfileEnterprise: {
			Profile: ProfileEnterprise,
			Branch: BranchConfig{
				AllowedTypes:         []string{"feature", "bugfix", "hotfix", "release", "chore"},
				RequireTicket:        false,
				MaxDescriptionLength: 100,
			},
			PR: PRConfig{
				RequiredSections: []string{
					"## Summary", "## Type", "## AI Disclosure",
					"## Changes", "## Testing", "## Checklist",
				},
				MinChanges: 1,
			},
			Commit: CommitConfig{
				AllowedTypes:    []string{"feat", "fix", "docs", "style", "refactor", "perf", "test", "build", "ci", "chore", "revert"},
				RequireScope:    true,
				MaxSubjectLength: 72,
			},
			AIDisclosure: AIDisclosure{
				Required:            true,
				StrictToolName:      true,
				RequireHumanReview:  true,
				AIBranchNaming:      "warning",
			},
			SeverityMap: map[string]Severity{
				"branch_type":      SeverityError,
				"branch_format":    SeverityError,
				"pr_sections":      SeverityError,
				"pr_ai_disclosure": SeverityError,
				"pr_ai_tool":       SeverityError,
				"commit_type":      SeverityError,
				"commit_scope":     SeverityWarning,
				"commit_ai":        SeverityError,
			},
		},
		ProfileRegulated: {
			Profile: ProfileRegulated,
			Branch: BranchConfig{
				AllowedTypes:         []string{"feature", "bugfix", "hotfix", "release", "chore"},
				RequireTicket:        true,
				MaxDescriptionLength: 80,
			},
			PR: PRConfig{
				RequiredSections: []string{
					"## Summary", "## Type", "## AI Disclosure",
					"## Related Issues", "## Changes", "## Testing",
					"## Risk Assessment", "## Checklist",
				},
				MinChanges: 1,
			},
			Commit: CommitConfig{
				AllowedTypes:    []string{"feat", "fix", "docs", "style", "refactor", "perf", "test", "build", "ci", "chore", "revert"},
				RequireScope:    true,
				MaxSubjectLength: 72,
			},
			AIDisclosure: AIDisclosure{
				Required:            true,
				StrictToolName:      true,
				RequireHumanReview:  true,
				AIBranchNaming:      "error",
			},
			SeverityMap: map[string]Severity{
				"branch_type":      SeverityError,
				"branch_format":    SeverityError,
				"branch_ticket":    SeverityError,
				"pr_sections":      SeverityError,
				"pr_ai_disclosure": SeverityError,
				"pr_ai_tool":       SeverityError,
				"commit_type":      SeverityError,
				"commit_scope":     SeverityError,
				"commit_ai":        SeverityError,
			},
		},
	}
}

// LoadPolicy discovers and loads an ODS policy from the repository root or user config.
// It merges: profile preset < user .ods.yaml < explicit overrides.
func LoadPolicy() (*Policy, error) {
	// Start with profile preset (default: enterprise)
	profileName := viper.GetString("profile")
	if profileName == "" || profileName == "l1" {
		profileName = ProfileEnterprise
	}

	presets := profilePresets()
	preset, ok := presets[profileName]
	if !ok {
		return nil, fmt.Errorf("unknown profile: %s (valid: oss, enterprise, regulated)", profileName)
	}

	// Merge user config from .ods.yaml on top of preset
	merged := preset
	if err := viper.UnmarshalKey("policy", &merged); err != nil {
		// Config file might not exist; that's OK
	}

	// Profile from viper takes precedence over config file
	if p := viper.GetString("profile"); p != "" && p != "l1" {
		merged.Profile = p
	}

	return &merged, nil
}

// LoadPolicyFromFile reads a policy from an explicit file path.
func LoadPolicyFromFile(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading policy file %s: %w", path, err)
	}

	// Handle the case where the file is a standalone policy YAML
	// (not under a top-level "policy:" key)
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("parsing policy file %s: %w", path, err)
	}

	// Try reading under "policy" key first, then root
	var p Policy
	if err := v.UnmarshalKey("policy", &p); err != nil || p.Profile == "" {
		if err := v.Unmarshal(&p); err != nil {
			return nil, fmt.Errorf("unmarshaling policy: %w", err)
		}
	}

	// Apply profile preset if specified
	profileName := p.Profile
	if profileName == "" {
		profileName = ProfileEnterprise
	}

	presets := profilePresets()
	preset, ok := presets[profileName]
	if !ok {
		return nil, fmt.Errorf("unknown profile: %s", profileName)
	}

	// Merge: preset as base, file values override
	merged := preset
	mergePolicies(&merged, &p)

	_ = data // used above via viper
	return &merged, nil
}

// DiscoverPolicyFile looks for an .ods.yaml file in the repo root.
func DiscoverPolicyFile() string {
	searchPaths := []string{
		".ods.yaml",
		".github/.ods.yaml",
		filepath.Join(os.Getenv("HOME"), ".config", "ods", "config.yaml"),
	}
	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// mergePolicies copies non-zero fields from src into dst.
func mergePolicies(dst, src *Policy) {
	if src.Profile != "" {
		dst.Profile = src.Profile
	}
	if len(src.Branch.AllowedTypes) > 0 {
		dst.Branch.AllowedTypes = src.Branch.AllowedTypes
	}
	if src.Branch.RequireTicket {
		dst.Branch.RequireTicket = true
	}
	if src.Branch.MaxDescriptionLength > 0 {
		dst.Branch.MaxDescriptionLength = src.Branch.MaxDescriptionLength
	}
	if len(src.PR.RequiredSections) > 0 {
		dst.PR.RequiredSections = src.PR.RequiredSections
	}
	if src.PR.MinChanges > 0 {
		dst.PR.MinChanges = src.PR.MinChanges
	}
	if len(src.Commit.AllowedTypes) > 0 {
		dst.Commit.AllowedTypes = src.Commit.AllowedTypes
	}
	if src.Commit.RequireScope {
		dst.Commit.RequireScope = true
	}
	if src.Commit.MaxSubjectLength > 0 {
		dst.Commit.MaxSubjectLength = src.Commit.MaxSubjectLength
	}
	if src.AIDisclosure.Required {
		dst.AIDisclosure.Required = true
	}
	if src.AIDisclosure.StrictToolName {
		dst.AIDisclosure.StrictToolName = true
	}
	if src.AIDisclosure.RequireHumanReview {
		dst.AIDisclosure.RequireHumanReview = true
	}
	if src.AIDisclosure.AIBranchNaming != "" {
		dst.AIDisclosure.AIBranchNaming = src.AIDisclosure.AIBranchNaming
	}
	if len(src.SeverityMap) > 0 {
		if dst.SeverityMap == nil {
			dst.SeverityMap = make(map[string]Severity)
		}
		for k, v := range src.SeverityMap {
			dst.SeverityMap[k] = v
		}
	}
}

// GetSeverity returns the severity level for a given rule ID.
func (p *Policy) GetSeverity(ruleID string) Severity {
	if p.SeverityMap != nil {
		if s, ok := p.SeverityMap[ruleID]; ok {
			return s
		}
	}
	// Default: all rules are errors except AI-related ones
	switch {
	case strings.Contains(ruleID, "ai"):
		return SeverityWarning
	default:
		return SeverityError
	}
}
