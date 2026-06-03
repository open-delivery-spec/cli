# AGENTS.md — ODS Compliance Instructions for AI Coding Agents

This file tells AI coding assistants (Claude Code, Cursor, GitHub Copilot, etc.)
how to comply with Open Delivery Spec (ODS) when contributing to this repository.

## Branch Naming

Create branches using the Conventional Branch format:

`<type>/<description>`

Valid types: feature, bugfix, hotfix, release, chore
Description must be lowercase, kebab-case (hyphens, no underscores, no spaces).

Examples:
- `feature/add-oauth-login`
- `bugfix/fix-null-pointer`
- `chore/update-dependencies`

You can generate a valid branch name with:
`ods generate branch --type feature --description "add-oauth-login"`

## Commit Messages

Use Conventional Commits format:

`<type>[scope]: <description>`

Valid types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert

### AI Disclosure in Commits

When AI assists in generating code, add these trailers to the commit footer:

```
AI-assisted: true
AI-tool: <tool-name>
AI-review: pending
AI-confidence: medium
```

Valid AI-tool values: GitHub Copilot, Claude, Cursor, etc.
Valid AI-review values: pending, passed, failed
Valid AI-confidence values: low, medium, high

## PR Description

Every PR must include these sections:

1. **## Summary** — 1-3 sentences about what and why
2. **## Type** — Feature, Bugfix, Hotfix, Refactor, Documentation, or Chore
3. **## AI Disclosure** — REQUIRED. Always include this section.
   - If AI was used: check the box and fill in AI Tool, AI Scope, Human Review
   - If no AI was used: include the section with the checkbox unchecked
4. **## Changes** — Bullet list of key changes
5. **## Testing** — What was tested and how
6. **## Checklist** — Standard compliance checklist

The AI Disclosure section must use qualitative descriptions:
- **AI Scope:** Describe what the AI generated (e.g., "auth module, token exchange logic, unit tests")
- **Human Review:** Describe what the human verified (e.g., "Verified against OAuth 2.0 spec, checked PKCE flow, reviewed error handling")
- Do NOT use percentage estimates — they are unreliable and misleading.

## Quick Reference

```bash
# Validate before committing
ods validate branch $(git branch --show-current)
ods validate commit --file <(git log -1 --format=%B)

# Generate compliant names and messages
ods generate branch --type feature --description "my-feature"
ods generate commit --type feat --scope auth --description "add login" --ai-tool "Claude"
ods generate pr

# Full compliance report
ods report
```

## Installation

```bash
# Install ODS CLI
go install github.com/open-delivery-spec/cli/cmd/ods@latest

# Initialize ODS in your repo
ods init

# Install git hooks for local validation
ods hook install
```
