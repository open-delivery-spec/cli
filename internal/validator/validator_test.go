package validator

import "testing"

func TestValidateBranch(t *testing.T) {
	tests := []struct {
		name   string
		want   ValidationStatus
		errors int
	}{
		{name: "feature/add-oauth-login", want: StatusConformant},
		{name: "main", want: StatusConformant},
		{name: "feature/ai-generated-client", want: StatusConformantWarnings},
		{name: "feature/Add_OAuth", want: StatusNonConformant, errors: 3},
		{name: "unknown/add-oauth-login", want: StatusNonConformant, errors: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateBranch(tt.name)
			if err != nil {
				t.Fatalf("ValidateBranch() error = %v", err)
			}
			if got.Status != tt.want {
				t.Fatalf("ValidateBranch() status = %s, want %s; result = %+v", got.Status, tt.want, got)
			}
			if len(got.Errors) < tt.errors {
				t.Fatalf("ValidateBranch() errors = %d, want at least %d; result = %+v", len(got.Errors), tt.errors, got)
			}
		})
	}
}

func TestValidateCommitMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want ValidationStatus
	}{
		{
			name: "plain conventional commit",
			msg:  "feat(auth): add oauth login",
			want: StatusConformant,
		},
		{
			name: "AI assisted commit with tool",
			msg: `feat(auth): add oauth login

AI-assisted: true
AI-tool: GitHub Copilot
AI-scope: auth module`,
			want: StatusConformant,
		},
		{
			name: "AI assisted commit missing tool",
			msg: `feat(auth): add oauth login

AI-assisted: true`,
			want: StatusNonConformant,
		},
		{
			name: "breaking change warning",
			msg: `feat(auth)!: replace session format

BREAKING CHANGE: session tokens must be rotated`,
			want: StatusConformantWarnings,
		},
		{
			name: "invalid type",
			msg:  "feature(auth): add oauth login",
			want: StatusNonConformant,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateCommitMessage(tt.msg)
			if err != nil {
				t.Fatalf("ValidateCommitMessage() error = %v", err)
			}
			if got.Status != tt.want {
				t.Fatalf("ValidateCommitMessage() status = %s, want %s; result = %+v", got.Status, tt.want, got)
			}
		})
	}
}

func TestValidatePRDescription(t *testing.T) {
	valid := `## Summary
Add OAuth login.

## Type
- [x] Feature

## AI Disclosure
- [x] This PR contains AI-generated code
- AI Tool: GitHub Copilot

## Changes
- Added provider integration.

## Testing
- Unit tests added.

## Checklist
- [x] Branch follows ODS.`

	got, err := ValidatePRDescription(valid)
	if err != nil {
		t.Fatalf("ValidatePRDescription() error = %v", err)
	}
	if got.Status != StatusConformant {
		t.Fatalf("ValidatePRDescription() status = %s, want %s; result = %+v", got.Status, StatusConformant, got)
	}

	missingTool := `## Summary
Add OAuth login.

## Type
- [x] Feature

## AI Disclosure
- [x] This PR contains AI-generated code

## Changes
- Added provider integration.

## Testing
- Unit tests added.

## Checklist
- [x] Branch follows ODS.`

	got, err = ValidatePRDescription(missingTool)
	if err != nil {
		t.Fatalf("ValidatePRDescription() error = %v", err)
	}
	if got.Status != StatusNonConformant {
		t.Fatalf("ValidatePRDescription() status = %s, want %s; result = %+v", got.Status, StatusNonConformant, got)
	}
}

func TestValidateAIReview(t *testing.T) {
	valid := `{
  "pr_number": 42,
  "review_level": "L2",
  "ai_contribution_percentage": 65,
  "reviewer": "jane-doe",
  "timestamp": "2026-05-25T10:00:00Z",
  "outcome": "approved",
  "checklist_results": {
    "correctness": { "passed": true, "issues": 0 },
    "security": { "passed": true, "issues": 0 },
    "ai_specific": { "passed": true, "issues": 0 },
    "quality": { "passed": true, "issues": 0 }
  }
}`

	got, err := ValidateAIReview(valid)
	if err != nil {
		t.Fatalf("ValidateAIReview() error = %v", err)
	}
	if got.Status != StatusConformant {
		t.Fatalf("ValidateAIReview() status = %s, want %s; result = %+v", got.Status, StatusConformant, got)
	}

	invalid := `{
  "pr_number": 42,
  "review_level": "L4",
  "reviewer": "jane-doe",
  "timestamp": "2026-05-25T10:00:00Z",
  "outcome": "pending",
  "checklist_results": {}
}`

	got, err = ValidateAIReview(invalid)
	if err != nil {
		t.Fatalf("ValidateAIReview() error = %v", err)
	}
	if got.Status != StatusNonConformant {
		t.Fatalf("ValidateAIReview() status = %s, want %s; result = %+v", got.Status, StatusNonConformant, got)
	}
}
