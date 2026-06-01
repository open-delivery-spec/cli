package validator

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/open-delivery-spec/cli/internal/policy"
)

//go:embed schemas/*
var embeddedSchemas embed.FS

// FixSuggestion represents a copy-paste fix template for a failed check.
type FixSuggestion struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Template    string `json:"template"`
}

// Result represents a validation result.
type Result struct {
	Status         ValidationStatus `json:"status"`
	Errors         []string         `json:"errors,omitempty"`
	Warnings       []string         `json:"warnings,omitempty"`
	FixSuggestions []FixSuggestion  `json:"fix_suggestions,omitempty"`
}

// ValidationStatus indicates conformity level.
type ValidationStatus string

const (
	StatusConformant         ValidationStatus = "conformant"
	StatusConformantWarnings ValidationStatus = "conformant_with_warnings"
	StatusNonConformant      ValidationStatus = "non_conformant"
)

func finalizeResult(result Result) Result {
	if result.Status == StatusConformant && len(result.Warnings) > 0 {
		result.Status = StatusConformantWarnings
	}
	return result
}

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

// ValidateBranch validates a branch name string using default policy.
func ValidateBranch(name string) (Result, error) {
	return ValidateBranchWithPolicy(name, nil)
}

// ValidateBranchWithPolicy validates a branch name against an ODS policy.
func ValidateBranchWithPolicy(name string, p *policy.Policy) (Result, error) {
	if p == nil {
		defaultP, _ := policy.LoadPolicy()
		p = defaultP
		if p == nil {
			p = &policy.Policy{
				Branch: policy.BranchConfig{
					AllowedTypes: []string{"feature", "feat", "bugfix", "fix", "hotfix", "release", "chore"},
				},
			}
		}
	}
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
		result.FixSuggestions = append(result.FixSuggestions, FixSuggestion{
			Title:       "Use correct branch format",
			Description: "Branch names must follow the <type>/<description> convention.",
			Template:    "feature/add-my-feature",
		})
		return result, nil
	}

	typ, desc := parts[0], parts[1]

	// validate type
	validTypes := make(map[string]bool)
	for _, t := range p.Branch.AllowedTypes {
		validTypes[t] = true
	}
	if !validTypes[typ] {
		errMsg := fmt.Sprintf("invalid branch type '%s' — must be one of: %s", typ, strings.Join(p.Branch.AllowedTypes, ", "))
		severity := p.GetSeverity("branch_type")
		if severity == policy.SeverityWarning {
			result.Warnings = append(result.Warnings, errMsg)
		} else {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, errMsg)
			result.FixSuggestions = append(result.FixSuggestions, FixSuggestion{
				Title:       "Use a valid branch type",
				Description: fmt.Sprintf("Replace '%s' with one of: %s", typ, strings.Join(p.Branch.AllowedTypes, ", ")),
				Template:    fmt.Sprintf("%s/%s", p.Branch.AllowedTypes[0], desc),
			})
		}
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
			result.FixSuggestions = append(result.FixSuggestions, FixSuggestion{
				Title:       "Lowercase the description",
				Description: "Convert all uppercase letters to lowercase.",
				Template:    fmt.Sprintf("%s/%s", typ, strings.ToLower(desc)),
			})
		}
		if strings.Contains(desc, "_") {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "description must not contain underscores")
			result.FixSuggestions = append(result.FixSuggestions, FixSuggestion{
				Title:       "Replace underscores with hyphens",
				Description: "Use hyphens instead of underscores.",
				Template:    fmt.Sprintf("%s/%s", typ, strings.ReplaceAll(desc, "_", "-")),
			})
		}
		if strings.Contains(desc, " ") {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "description must not contain spaces")
			result.FixSuggestions = append(result.FixSuggestions, FixSuggestion{
				Title:       "Replace spaces with hyphens",
				Description: "Use hyphens instead of spaces.",
				Template:    fmt.Sprintf("%s/%s", typ, strings.ReplaceAll(desc, " ", "-")),
			})
		}
		if strings.Contains(desc, "--") {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "description must not contain consecutive hyphens")
		}
		if strings.HasPrefix(desc, "-") || strings.HasSuffix(desc, "-") {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "description must not have leading or trailing hyphens")
			result.FixSuggestions = append(result.FixSuggestions, FixSuggestion{
				Title:       "Remove leading/trailing hyphens",
				Description: "Trim hyphens from the start and end of the description.",
				Template:    fmt.Sprintf("%s/%s", typ, strings.Trim(desc, "-")),
			})
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

		// AI marker detection
		if strings.HasPrefix(desc, "ai-") {
			aiSeverity := p.AIDisclosure.AIBranchNaming
			if aiSeverity == "" {
				aiSeverity = "warning"
			}
			switch aiSeverity {
			case "error", "deny":
				result.Status = StatusNonConformant
				result.Errors = append(result.Errors, "AI-prefixed branches are not allowed per policy")
			case "warning":
				result.Warnings = append(result.Warnings, "branch has AI marker — consider enhanced review")
			}
		}

		// Ticket requirement
		if p.Branch.RequireTicket {
			ticketPattern := `^[A-Z]+-\d+`
			if matched, _ := regexp.MatchString(ticketPattern, desc); !matched {
				result.Status = StatusNonConformant
				result.Errors = append(result.Errors, "branch requires a ticket ID (e.g., PROJ-123)")
				result.FixSuggestions = append(result.FixSuggestions, FixSuggestion{
					Title:       "Add ticket ID",
					Description: "Prefix the description with your ticket ID.",
					Template:    fmt.Sprintf("%s/PROJ-123-%s", typ, desc),
				})
			}
		}

		// Max description length
		if p.Branch.MaxDescriptionLength > 0 && len(desc) > p.Branch.MaxDescriptionLength {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"description is %d characters (max %d)", len(desc), p.Branch.MaxDescriptionLength))
		}
	}

	return finalizeResult(result), nil
}

// ValidateCommitMessage validates a commit message string using default policy.
func ValidateCommitMessage(msg string) (Result, error) {
	return ValidateCommitMessageWithPolicy(msg, nil)
}

// ValidateCommitMessageWithPolicy validates a commit message against an ODS policy.
func ValidateCommitMessageWithPolicy(msg string, p *policy.Policy) (Result, error) {
	if p == nil {
		defaultP, _ := policy.LoadPolicy()
		p = defaultP
		if p == nil {
			p = &policy.Policy{
				Commit: policy.CommitConfig{
					AllowedTypes:    []string{"feat", "fix", "docs", "style", "refactor", "perf", "test", "build", "ci", "chore", "revert"},
					RequireScope:    false,
					MaxSubjectLength: 100,
				},
			}
		}
	}
	result := Result{Status: StatusConformant}
	lines := strings.Split(strings.TrimSpace(msg), "\n")

	if len(lines) == 0 || lines[0] == "" {
		result.Status = StatusNonConformant
		result.Errors = append(result.Errors, "commit message must not be empty")
		return result, nil
	}

	// parse first line: type(scope): description or type!: description
	firstLine := lines[0]

	// Build type pattern from policy
	typeList := strings.Join(p.Commit.AllowedTypes, "|")
	scopePart := `(\([a-z0-9_-]+\))?`
	if p.Commit.RequireScope {
		scopePart = `\([a-z0-9_-]+\)`
	}
	typePattern := fmt.Sprintf(`^(%s)%s!?: .+`, typeList, scopePart)
	if matched, _ := regexp.MatchString(typePattern, firstLine); !matched {
		result.Status = StatusNonConformant
		errDetail := fmt.Sprintf("first line must follow conventional commit format: <type>[scope]: <description>\n  Got: %s", firstLine)
		result.Errors = append(result.Errors, errDetail)
		allowedList := strings.Join(p.Commit.AllowedTypes, ", ")
		var template string
		if p.Commit.RequireScope {
			template = fmt.Sprintf("%s(area): brief description", p.Commit.AllowedTypes[0])
		} else {
			template = fmt.Sprintf("%s: brief description", p.Commit.AllowedTypes[0])
		}
		result.FixSuggestions = append(result.FixSuggestions, FixSuggestion{
			Title:       "Use conventional commit format",
			Description: fmt.Sprintf("Valid types: %s. Format: <type>[scope]: <description>", allowedList),
			Template:    template,
		})
	}

	// check subject length
	if p.Commit.MaxSubjectLength > 0 && len(firstLine) > p.Commit.MaxSubjectLength {
		severity := p.GetSeverity("commit_format")
		errMsg := fmt.Sprintf("subject is %d characters (max %d)", len(firstLine), p.Commit.MaxSubjectLength)
		if severity == policy.SeverityWarning {
			result.Warnings = append(result.Warnings, errMsg)
		} else {
			result.Errors = append(result.Errors, errMsg)
		}
		result.FixSuggestions = append(result.FixSuggestions, FixSuggestion{
			Title:       "Shorten the commit subject",
			Description: fmt.Sprintf("Keep the subject line under %d characters.", p.Commit.MaxSubjectLength),
			Template:    firstLine[:p.Commit.MaxSubjectLength-3] + "...",
		})
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
		aiSeverity := p.GetSeverity("commit_ai")
		errMsg := "AI-assisted: true requires AI-tool to be specified"
		if aiSeverity == policy.SeverityWarning {
			result.Warnings = append(result.Warnings, errMsg)
		} else {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, errMsg)
			result.FixSuggestions = append(result.FixSuggestions, FixSuggestion{
				Title:       "Add AI tool name",
				Description: "When AI-assisted: true is set, you must specify which AI tool was used.",
				Template:    "AI-assisted: true\nAI-tool: <tool-name>",
			})
		}
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

	return finalizeResult(result), nil
}

// ValidatePRDescription validates a PR description string using default policy.
func ValidatePRDescription(body string) (Result, error) {
	return ValidatePRDescriptionWithPolicy(body, nil)
}

// ValidatePRDescriptionWithPolicy validates a PR description against an ODS policy.
func ValidatePRDescriptionWithPolicy(body string, p *policy.Policy) (Result, error) {
	if p == nil {
		defaultP, _ := policy.LoadPolicy()
		p = defaultP
		if p == nil {
			p = &policy.Policy{
				PR: policy.PRConfig{
					RequiredSections: []string{"## Summary", "## Type", "## AI Disclosure", "## Changes", "## Testing", "## Checklist"},
					MinChanges:       1,
				},
			}
		}
	}
	result := Result{Status: StatusConformant}

	missingSections := []string{}
	for _, section := range p.PR.RequiredSections {
		if !strings.Contains(body, section) {
			missingSections = append(missingSections, section)
		}
	}

	if len(missingSections) > 0 {
		severity := p.GetSeverity("pr_sections")
		errMsg := fmt.Sprintf("missing required section(s): %s", strings.Join(missingSections, ", "))
		if severity == policy.SeverityWarning {
			result.Warnings = append(result.Warnings, errMsg)
		} else {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, errMsg)
			// Build fix template for missing sections
			var templateBuilder strings.Builder
			templateBuilder.WriteString("Copy and add the missing section(s) to your PR description:\n\n")
			for _, section := range missingSections {
				switch section {
				case "## Summary":
					templateBuilder.WriteString("## Summary\n[One sentence describing what this PR does]\n\n")
				case "## Type":
					templateBuilder.WriteString("## Type\n- [ ] Feature\n- [ ] Bugfix\n- [ ] Hotfix\n- [ ] Refactor\n- [ ] Documentation\n- [ ] Chore\n\n")
				case "## AI Disclosure":
					templateBuilder.WriteString("## AI Disclosure\n- [ ] This PR contains AI-generated code\n- **AI Tool:** [e.g., GitHub Copilot]\n- **AI Scope:** [What the AI generated]\n- **Human Review:** [What the human reviewed]\n\n")
				case "## Related Issues":
					templateBuilder.WriteString("## Related Issues\nCloses #[issue-number]\n\n")
				case "## Changes":
					templateBuilder.WriteString("## Changes\n- [change 1]\n- [change 2]\n\n")
				case "## Testing":
					templateBuilder.WriteString("## Testing\n- [ ] Unit tests added/updated\n- [ ] Integration tests added/updated\n- [ ] Manual testing performed\n\n")
				case "## Risk Assessment":
					templateBuilder.WriteString("## Risk Assessment\n- **Deployment risk:** [Low / Medium / High]\n- **Rollback plan:** [description]\n- **Breaking change:** [Yes / No]\n\n")
				case "## Checklist":
					templateBuilder.WriteString("## Checklist\n- [ ] Branch naming follows ODS\n- [ ] Commits follow ODS\n- [ ] AI-generated code has been reviewed by a human\n- [ ] No secrets, tokens, or credentials are included\n- [ ] Documentation has been updated\n\n")
				}
			}
			result.FixSuggestions = append(result.FixSuggestions, FixSuggestion{
				Title:       "Add missing PR sections",
				Description: fmt.Sprintf("Your PR is missing %d required section(s).", len(missingSections)),
				Template:    strings.TrimSpace(templateBuilder.String()),
			})
		}
	}

	// AI disclosure check
	if strings.Contains(body, "This PR contains AI-generated code") {
		if !strings.Contains(body, "AI Tool:") && !strings.Contains(body, "**AI Tool:") {
			aiSeverity := p.GetSeverity("pr_ai_tool")
			errMsg := "AI disclosure requires 'AI Tool:' field"
			if aiSeverity == policy.SeverityWarning {
				result.Warnings = append(result.Warnings, errMsg)
			} else {
				result.Status = StatusNonConformant
				result.Errors = append(result.Errors, errMsg)
				result.FixSuggestions = append(result.FixSuggestions, FixSuggestion{
					Title:       "Add AI Tool name",
					Description: "When AI-generated code is disclosed, the AI tool name is required.",
					Template:    "- **AI Tool:** [e.g., GitHub Copilot, Claude, Cursor]",
				})
			}
		}
	} else if p.AIDisclosure.Required {
		aiSeverity := p.GetSeverity("pr_ai_disclosure")
		errMsg := "AI Disclosure section or 'This PR contains AI-generated code' checkbox required"
		if aiSeverity == policy.SeverityWarning {
			result.Warnings = append(result.Warnings, errMsg)
		} else {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, errMsg)
			result.FixSuggestions = append(result.FixSuggestions, FixSuggestion{
				Title:       "Add AI Disclosure",
				Description: "Your policy requires AI disclosure in all PRs.",
				Template:    "## AI Disclosure\n- [ ] This PR contains AI-generated code\n- **AI Tool:** [e.g., GitHub Copilot]\n- **AI Scope:** [What the AI generated]\n- **Human Review:** [What the human reviewed]",
			})
		}
	}

	return finalizeResult(result), nil
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

	return finalizeResult(result), nil
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

	return finalizeResult(result), nil
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

	return finalizeResult(result), nil
}

// ValidateAIReview validates an AI change review record against the ODS schema.
func ValidateAIReview(body string) (Result, error) {
	result := Result{Status: StatusConformant}

	var review map[string]interface{}
	if err := json.Unmarshal([]byte(body), &review); err != nil {
		result.Status = StatusNonConformant
		result.Errors = append(result.Errors, fmt.Sprintf("invalid JSON: %v", err))
		return result, nil
	}

	requiredFields := []string{"pr_number", "review_level", "reviewer", "timestamp", "outcome", "checklist_results"}
	for _, field := range requiredFields {
		if _, ok := review[field]; !ok {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, fmt.Sprintf("missing required field: %s", field))
		}
	}

	// validate review_level
	if level, ok := review["review_level"].(string); ok {
		validLevels := map[string]bool{"L1": true, "L2": true, "L3": true}
		if !validLevels[level] {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, fmt.Sprintf("review_level must be L1, L2, or L3. Got: %s", level))
		}
	}

	// validate outcome
	if outcome, ok := review["outcome"].(string); ok {
		validOutcomes := map[string]bool{
			"approved":              true,
			"approved_with_changes": true,
			"changes_requested":     true,
			"blocked":               true,
		}
		if !validOutcomes[outcome] {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, fmt.Sprintf("outcome must be one of: approved, approved_with_changes, changes_requested, blocked. Got: %s", outcome))
		}
	}

	// validate checklist_results
	if checklist, ok := review["checklist_results"].(map[string]interface{}); ok {
		checklistCategories := []string{"correctness", "security", "ai_specific", "quality"}
		for _, cat := range checklistCategories {
			item, ok := checklist[cat].(map[string]interface{})
			if !ok {
				result.Status = StatusNonConformant
				result.Errors = append(result.Errors, fmt.Sprintf("checklist_results must contain '%s' object", cat))
				continue
			}
			if _, ok := item["passed"]; !ok {
				result.Status = StatusNonConformant
				result.Errors = append(result.Errors, fmt.Sprintf("checklist_results.%s must contain 'passed' field", cat))
			}
			if _, ok := item["issues"]; !ok {
				result.Status = StatusNonConformant
				result.Errors = append(result.Errors, fmt.Sprintf("checklist_results.%s must contain 'issues' field", cat))
			}
		}
	}

	// L3 requires second_reviewer
	if level, _ := review["review_level"].(string); level == "L3" {
		if _, ok := review["second_reviewer"]; !ok {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "L3 reviews require a second_reviewer")
		}
	}

	// approved_with_changes and changes_requested require human_modifications
	if outcome, _ := review["outcome"].(string); outcome == "approved_with_changes" || outcome == "changes_requested" {
		mods, ok := review["human_modifications"]
		if !ok {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "outcome 'approved_with_changes' or 'changes_requested' requires human_modifications list")
		} else if modsArr, ok := mods.([]interface{}); ok && len(modsArr) == 0 {
			result.Warnings = append(result.Warnings, "human_modifications list is empty")
		}
	}

	// validate issues_found items
	if issues, ok := review["issues_found"].([]interface{}); ok {
		for i, issue := range issues {
			if issueMap, ok := issue.(map[string]interface{}); ok {
				for _, field := range []string{"category", "severity", "description"} {
					if _, ok := issueMap[field]; !ok {
						result.Status = StatusNonConformant
						result.Errors = append(result.Errors, fmt.Sprintf("issues_found[%d] missing required field: %s", i, field))
					}
				}
				if severity, ok := issueMap["severity"].(string); ok {
					validSeverities := map[string]bool{"critical": true, "high": true, "medium": true, "low": true, "info": true}
					if !validSeverities[severity] {
						result.Status = StatusNonConformant
						result.Errors = append(result.Errors, fmt.Sprintf("issues_found[%d] severity must be one of: critical, high, medium, low, info. Got: %s", i, severity))
					}
				}
			}
		}
	}

	// ai_contribution_percentage range check
	if pct, ok := review["ai_contribution_percentage"].(float64); ok {
		if pct < 0 || pct > 100 {
			result.Status = StatusNonConformant
			result.Errors = append(result.Errors, "ai_contribution_percentage must be between 0 and 100")
		}
	}

	return finalizeResult(result), nil
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

	return finalizeResult(result), nil
}
