package validator

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed schemas/*
var embeddedSchemas embed.FS

// Result represents a validation result.
type Result struct {
	Status   ValidationStatus `json:"status"`
	Errors   []string         `json:"errors,omitempty"`
	Warnings []string         `json:"warnings,omitempty"`
}

// ValidationStatus indicates conformity level.
type ValidationStatus string

const (
	StatusConformant         ValidationStatus = "conformant"
	StatusConformantWarnings ValidationStatus = "conformant_with_warnings"
	StatusNonConformant      ValidationStatus = "non_conformant"
)

// schema cache
var schemas = map[string]interface{}{}

// LoadEmbeddedSchemas loads schemas bundled with the CLI.
func LoadEmbeddedSchemas() error {
	schemaNames := []string{
		"branch-naming", "commit-message", "pr-description",
		"ci-failure", "release-readiness", "rollback-plan",
		"prod-release-evidence", "ai-change-review", "approval-workflow",
	}
	for _, name := range schemaNames {
		data, err := embeddedSchemas.ReadFile("schemas/" + name + ".json")
		if err != nil {
			return fmt.Errorf("loading embedded schema %s: %w", name, err)
		}
		var schema interface{}
		if err := json.Unmarshal(data, &schema); err != nil {
			return fmt.Errorf("parsing schema %s: %w", name, err)
		}
		schemas[name] = schema
	}
	return nil
}

// LoadSchemasFromDir loads schemas from a directory.
func LoadSchemasFromDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading schema dir: %w", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		var schema interface{}
		if err := json.Unmarshal(data, &schema); err != nil {
			return fmt.Errorf("parsing %s: %w", name, err)
		}
		schemas[name] = schema
	}
	return nil
}

// ValidateBranch validates a branch name string.
func ValidateBranch(name string) (Result, error) {
	result := Result{Status: StatusConformant}

	// trunk branches
	if name == "main" || name == "master" || name == "develop" {
		return result, nil
	}

	// must have type/description format
	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 {
		result.Status = StatusNonConformant
		result.Errors = append(result.Errors, "branch name must be in format <type>/<description>")
		return result, nil
	}

	typ, desc := parts[0], parts[1]

	// validate type
	validTypes := map[string]bool{
		"feature": true, "feat": true, "bugfix": true, "fix": true,
		"hotfix": true, "release": true, "chore": true,
	}
	if !validTypes[typ] {
		result.Status = StatusNonConformant
		result.Errors = append(result.Errors, fmt.Sprintf("invalid branch type '%s' — must be one of: feature, bugfix, hotfix, release, chore", typ))
	}

	// validate description
	if desc == "" {
		result.Status = StatusNonConformant
		result.Errors = append(result.Errors, "description must not be empty")
	} else {
		// must be lowercase, kebab-case
		if strings.ToLower(desc) != desc {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "description must be lowercase")
		}
		if strings.Contains(desc, "_") {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "description must not contain underscores")
		}
		if strings.Contains(desc, " ") {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "description must not contain spaces")
		}
		if strings.Contains(desc, "--") {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "description must not contain consecutive hyphens")
		}
		if strings.HasPrefix(desc, "-") || strings.HasSuffix(desc, "-") {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "description must not have leading or trailing hyphens")
		}

		// validate kebab-case pattern (alphanumeric segments separated by hyphens)
		pattern := `^[a-z0-9]+(-[a-z0-9]+)*$`
		if typ == "release" {
			// allow dots for version
			pattern = `^v?[a-z0-9]+(\.[a-z0-9]+)*(-[a-z0-9]+)*$`
		}
		if matched, _ := regexp.MatchString(pattern, desc); !matched {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, fmt.Sprintf("description '%s' does not match required kebab-case format", desc))
		}

		// AI marker detection (warning only)
		if strings.HasPrefix(desc, "ai-") {
			result.Warnings = append(result.Warnings, "branch has AI marker — consider enhanced review")
		}
	}

	return result, nil
}

// ValidateCommitMessage validates a commit message string.
func ValidateCommitMessage(msg string) (Result, error) {
	result := Result{Status: StatusConformant}
	lines := strings.Split(strings.TrimSpace(msg), "\n")

	if len(lines) == 0 || lines[0] == "" {
		result.Status = StatusNonConformant
		result.Errors = append(result.Errors, "commit message must not be empty")
		return result, nil
	}

	// parse first line: type(scope): description or type!: description
	firstLine := lines[0]
	typePattern := `^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9_-]+\))?!?: .+`
	if matched, _ := regexp.MatchString(typePattern, firstLine); !matched {
		result.Status = StatusNonConformant
		result.Errors = append(result.Errors, fmt.Sprintf("first line must follow conventional commit format: <type>[scope]: <description>\n  Got: %s", firstLine))
	}

	// check for breaking change
	if strings.Contains(msg, "BREAKING CHANGE:") || strings.Contains(firstLine, "!") {
		result.Warnings = append(result.Warnings, "breaking change detected — ensure migration guide exists")
	}

	// AI fields validation (in footer)
	footerAI := false
	footerAITool := false
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "AI-assisted: true" {
			footerAI = true
		}
		if strings.HasPrefix(strings.TrimSpace(line), "AI-tool:") {
			footerAITool = true
		}
	}
	if footerAI && !footerAITool {
		result.Status = StatusNonConformant
		result.Errors = append(result.Errors, "AI-assisted: true requires AI-tool to be specified")
	}

	// AI review field validation
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "AI-review:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "AI-review:"))
			if val != "pending" && val != "passed" && val != "failed" {
				result.Warnings = append(result.Warnings, fmt.Sprintf("AI-review must be one of: pending, passed, failed. Got: %s", val))
			}
		}
		if strings.HasPrefix(trimmed, "AI-confidence:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "AI-confidence:"))
			if val != "low" && val != "medium" && val != "high" {
				result.Warnings = append(result.Warnings, fmt.Sprintf("AI-confidence must be one of: low, medium, high. Got: %s", val))
			}
		}
	}

	return result, nil
}

// ValidatePRDescription validates a PR description string.
func ValidatePRDescription(body string) (Result, error) {
	result := Result{Status: StatusConformant}

	requiredSections := []string{"## Summary", "## Type", "## AI Disclosure", "## Changes", "## Testing", "## Checklist"}
	for _, section := range requiredSections {
		if !strings.Contains(body, section) {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, fmt.Sprintf("missing required section: %s", section))
		}
	}

	// AI disclosure check
	if strings.Contains(body, "This PR contains AI-generated code") {
		if !strings.Contains(body, "AI Tool:") {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "AI disclosure requires 'AI Tool:' field")
		}
	}

	return result, nil
}

// ValidateRollbackPlan validates a rollback plan JSON.
func ValidateRollbackPlan(body string) (Result, error) {
	result := Result{Status: StatusConformant}

	var plan map[string]interface{}
	if err := json.Unmarshal([]byte(body), &plan); err != nil {
		result.Status = StatusNonConformant
		result.Errors = append(result.Errors, fmt.Sprintf("invalid JSON: %v", err))
		return result, nil
	}

	requiredFields := []string{"release_id", "rollback_strategy", "steps", "rollback_indicators", "data_rollback", "communication_plan"}
	for _, field := range requiredFields {
		if _, ok := plan[field]; !ok {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, fmt.Sprintf("missing required field: %s", field))
		}
	}

	// validate steps
	if steps, ok := plan["steps"].([]interface{}); ok {
		if len(steps) == 0 {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "rollback plan must have at least one step")
		}
		for _, s := range steps {
			if step, ok := s.(map[string]interface{}); ok {
				if _, ok := step["verification"]; !ok {
					result.Status = StatusNonConformant
					result.Errors = append(result.Errors, "each step must include verification")
				}
			}
		}
	}

	return result, nil
}

// ValidateEvidence validates a production release evidence bundle.
func ValidateEvidence(body string) (Result, error) {
	result := Result{Status: StatusConformant}

	var bundle map[string]interface{}
	if err := json.Unmarshal([]byte(body), &bundle); err != nil {
		result.Status = StatusNonConformant
		result.Errors = append(result.Errors, fmt.Sprintf("invalid JSON: %v", err))
		return result, nil
	}

	requiredFields := []string{"bundle_id", "release_id", "environment", "deployed_at", "evidence"}
	for _, field := range requiredFields {
		if _, ok := bundle[field]; !ok {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, fmt.Sprintf("missing required field: %s", field))
		}
	}

	// environment must be production
	if env, ok := bundle["environment"].(string); ok && env != "production" {
		result.Warnings = append(result.Warnings, "evidence bundle environment should be 'production'")
	}

	return result, nil
}

// ValidateReleaseReadiness validates a release readiness report JSON.
func ValidateReleaseReadiness(body string) (Result, error) {
	result := Result{Status: StatusConformant}

	var report map[string]interface{}
	if err := json.Unmarshal([]byte(body), &report); err != nil {
		result.Status = StatusNonConformant
		result.Errors = append(result.Errors, fmt.Sprintf("invalid JSON: %v", err))
		return result, nil
	}

	requiredFields := []string{"release_id", "target_environment", "gates"}
	for _, field := range requiredFields {
		if _, ok := report[field]; !ok {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, fmt.Sprintf("missing required field: %s", field))
		}
	}

	// score check
	if score, ok := report["overall_score"].(float64); ok {
		if score < 80 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("readiness score %.0f is below 80 threshold", score))
		}
	}

	return result, nil
}

// ValidateApprovalPolicy validates an approval policy JSON.
func ValidateApprovalPolicy(body string) (Result, error) {
	result := Result{Status: StatusConformant}

	var policy map[string]interface{}
	if err := json.Unmarshal([]byte(body), &policy); err != nil {
		result.Status = StatusNonConformant
		result.Errors = append(result.Errors, fmt.Sprintf("invalid JSON: %v", err))
		return result, nil
	}

	requiredFields := []string{"policy_id", "version", "rules", "roles"}
	for _, field := range requiredFields {
		if _, ok := policy[field]; !ok {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, fmt.Sprintf("missing required field: %s", field))
		}
	}

	// validate rules
	if rules, ok := policy["rules"].([]interface{}); ok {
		if len(rules) == 0 {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "policy must have at least one rule")
		}
	}

	return result, nil
}
