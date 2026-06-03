# ODS Compliance Checks Reference

Open Delivery Spec defines 10 compliance checks across four severity tiers.
Each check has a weight that determines its contribution to the overall score (0–100).

## Scoring Model

| Severity | Weight | Checks |
|----------|--------|--------|
| **Critical** | 10 | `ai-disclosure`, `human-review-evidence` |
| **High** | 7 | `required-ci`, `approval-policy`, `ai-agent-commit-detection`, `test-evidence`, `security-scan-evidence` |
| **Medium** | 5 | `pr-description`, `release-readiness` |
| **Low** | 2 | `commit-message` |

Skipped checks (insufficient data) are excluded from the score calculation.

## Quick Reference

| # | Check | Weight | What it checks |
|---|-------|--------|----------------|
| 1 | [AI Disclosure](#1-ai-disclosure) | 10 | AI-generated code is disclosed in commits and PRs |
| 2 | [Human Review Evidence](#2-human-review-evidence) | 10 | PRs have been reviewed by a real human |
| 3 | [Required CI](#3-required-ci) | 7 | CI pipeline is configured |
| 4 | [Approval Policy](#4-approval-policy) | 7 | Branch protection rules are configured |
| 5 | [AI Agent Commit Detection](#5-ai-agent-commit-detection) | 7 | AI agent commits are detected and flagged |
| 6 | [Test Evidence](#6-test-evidence) | 7 | Tests are present in changed code |
| 7 | [Security Scan Evidence](#7-security-scan-evidence) | 7 | Security scanning is in CI |
| 8 | [PR Description](#8-pr-description) | 5 | PR descriptions have required sections |
| 9 | [Release Readiness](#9-release-readiness) | 5 | Release process integrates ODS |
| 10 | [Commit Message](#10-commit-message) | 2 | Commits follow Conventional Commits |

---

## 1. AI Disclosure

**Weight:** 10 | **Severity:** Critical

### What it measures

Detects whether AI-generated code is properly disclosed in:
- Commit message trailers (`AI-assisted: true`, `AI-tool: <name>`, `Co-authored-by:`, `Assisted-By:`)
- PR description AI Disclosure section
- `.ods.yaml` AI disclosure policy configuration

### Why it matters

Without AI disclosure, you cannot track which code was AI-generated — making it impossible to audit AI's safety impact, review quality, or attribution. This is the foundation check for all other AI safety measures.

### How to fix failures

**In commit messages:**
```
AI-assisted: true
AI-tool: GitHub Copilot
```

**In PR descriptions:**
```markdown
## AI Disclosure
- [x] This PR contains AI-generated code
- **AI Tool:** GitHub Copilot
- **AI Scope:** auth module, token exchange
- **Human Review:** Verified OAuth spec compliance, PKCE handling
```

### Policy configuration

```yaml
ai_disclosure:
  required: true
  strict_tool_name: true
  require_human_review: true
```

---

## 2. Human Review Evidence

**Weight:** 10 | **Severity:** Critical

### What it measures

Verifies that PRs have been reviewed by at least one human (non-bot) reviewer. Detects:
- Bot-only reviews (no human input)
- Self-approvals
- AI agent PRs with no human oversight

### Why it matters

80% of PRs with AI review tools have zero human comments. "PR approved" ≠ "someone actually read the code." This is the highest-ROI check in the AI era.

### How to fix failures

1. Request review from a human team member
2. Ensure they leave a meaningful review comment (not just an approval)
3. For AI agent PRs: mandatory human review before merge

---

## 3. Required CI

**Weight:** 7 | **Severity:** High

### What it measures

Checks that a CI pipeline is configured and triggers on `pull_request` events.

### Why it matters

AI-generated code needs the same basic safety net as human code. No CI = no automated testing, no linting, no build verification.

### How to fix failures

Create `.github/workflows/ci.yml`:
```yaml
name: CI
on:
  pull_request:
    types: [opened, synchronize, reopened]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: make test
```

---

## 4. Approval Policy

**Weight:** 7 | **Severity:** High

### What it measures

Checks that branch protection rules are configured:
- Required pull request reviews before merging
- CODEOWNERS file for critical paths

### Why it matters

Policy-layer check. Even with human review evidence, you need rules that enforce reviews before merge. Policy + Evidence = defense in depth.

### How to fix failures

1. Enable branch protection: Settings → Branches → Add rule for `main`
2. Add `CODEOWNERS` file for critical paths

---

## 5. AI Agent Commit Detection

**Weight:** 7 | **Severity:** High

### What it measures

Detects commits authored by AI agents by analyzing:
- Commit author email patterns (Copilot, Claude, Cursor, etc.)
- PR author username patterns

Detected agents include: GitHub Copilot, Claude, Cursor, Codeium, SWE-agent, Aider, GPT Pilot, and more.

### Why it matters

AI agent commits without human review are the highest-risk scenario. Detection enables targeted review requirements.

### How to fix failures

When AI agent commits are detected:
1. Ensure each commit has a corresponding human review
2. Add AI disclosure to the commit or PR
3. Consider requiring multi-person review for AI agent PRs

---

## 6. Test Evidence

**Weight:** 7 | **Severity:** High

### What it measures

Detects whether test files are present alongside changed source code. Checks for:
- Test file patterns (`*_test.go`, `*.test.ts`, `__tests__/`, etc.)
- CI workflow test steps

### Why it matters

AI-generated code most commonly lacks tests, especially security edge cases and boundary conditions. This check ensures visible test evidence.

### How to fix failures

1. Add test files alongside changed source files
2. Ensure CI runs tests automatically (`go test ./...`, `npm test`, etc.)

---

## 7. Security Scan Evidence

**Weight:** 7 | **Severity:** High

### What it measures

Detects security scanning tools integrated in CI:
- CodeQL, Snyk, Semgrep, Trivy, Gosec
- Gitleaks, TruffleHog, OWASP ZAP
- Container, Kubernetes, and infrastructure scanners

### Why it matters

25% of AI-generated code contains confirmed security vulnerabilities. A security scan in CI is the minimum defense.

### How to fix failures

Add a security scanning step to your CI pipeline:
```yaml
- name: Run CodeQL
  uses: github/codeql-action/analyze@v3
```

---

## 8. PR Description

**Weight:** 5 | **Severity:** Medium

### What it measures

Validates PR descriptions include required sections:
- Summary, Type, AI Disclosure
- Changes, Testing, Checklist

### Why it matters

A well-structured PR description with AI disclosure creates an audit trail and sets clear expectations for reviewers.

### How to fix failures

Use the PR template. Include all required sections, especially AI Disclosure when AI-generated code is present.

---

## 9. Release Readiness

**Weight:** 5 | **Severity:** Medium

### What it measures

Checks that the release process integrates ODS compliance checks:
- ODS report in CI/CD pipeline
- Release gates include AI disclosure and review requirements

### Why it matters

ODS checks should be release gates, not just PR checks. Without release integration, non-compliant code can still ship.

### How to fix failures

1. Add `ods report` to your release pipeline
2. Include ODS results as a release gate
3. Check for `ods-report/` directory in CI

---

## 10. Commit Message

**Weight:** 2 | **Severity:** Low

### What it measures

Validates commit messages follow Conventional Commits format:
- `<type>[scope]: <description>`
- Optional AI attribution trailers

### Why it matters

Structured commit metadata enables automated tracking of AI code percentage, fault attribution, and review chains.

### How to fix failures

Format commits as:
```
feat(auth): add OAuth login

AI-assisted: true
AI-tool: GitHub Copilot
```
