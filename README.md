# ODS CLI

> **The CI gate that knows whether a human actually reviewed the AI code.**

[![CI](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml/badge.svg)](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

> **Dogfooding:** This repository validates its own PRs with ODS.

---

## The Problem

AI makes writing code fast. Everything after AI — review, verification, audit — is now harder:

| What AI changed | Why it's a problem |
|---|---|
| **Code velocity ↑** | PR volume grows 3-5× while review capacity stays flat |
| **Review fatigue** | 80% of AI-assisted PRs have zero human comments |
| **Attribution vacuum** | Six months later, nobody knows what came from AI vs human |
| **Hallucination in prod** | AI-invents APIs, packages, configs that slip past tests |
| **Security blind spots** | 25% of AI-generated code has confirmed vulnerabilities |

**ODS addresses these by making AI contribution visible, verifiable, and auditable** — not by blocking AI, but by ensuring every piece of AI-generated code has a human who certifies they reviewed it.

---

## What ODS Actually Does

ODS is **not** a code quality tool, a linter, or a test framework. It is a **delivery governance layer** for the AI era:

1. **Detects AI-generated code** in commits and PRs (via trailers, disclosure sections, agent patterns)
2. **Verifies human review actually happened** (not just approval — actual review with evidence)
3. **Detects AI hallucinations in CI failures** (non-existent symbols, wrong imports, fake URLs)
4. **Enforces structured delivery artifacts** (branch naming, commit messages, PR descriptions)
5. **Produces auditable compliance reports** for governance and compliance teams

---

## Quick Start

```bash
# Install
go install github.com/open-delivery-spec/cli/cmd/ods@latest

# Initialize your repo (one command)
ods init

# Install pre-commit hooks for instant feedback
ods hook install

# Run a compliance report
ods report

# Get fix suggestions
ods fix
```

---

## Command Reference

### Production Commands

| Command | What it does |
|---|---|
| `ods init` | Scaffold ODS config, PR template, CI workflows, AGENTS.md |
| `ods hook install` | Install pre-commit, commit-msg, pre-push hooks |
| `ods report` | Generate multi-format compliance report (10 checks, 0-100 score) |
| `ods fix` | Generate and apply fix suggestions for compliance issues |
| `ods badge` | Generate shields.io JSON for dynamic compliance badge |
| `ods checks list` | List all 10 compliance checks |
| `ods checks explain <id>` | Detailed check documentation |

### Validate Commands

| Command | What it validates |
|---|---|
| `ods validate branch <name>` | Branch naming (Conventional Branch) |
| `ods validate commit --file <path>` | Commit message (Conventional Commits + AI trailers) |
| `ods validate pr --file <path>` | PR description (required sections + AI Disclosure) |
| `ods validate rollback --file <path>` | Rollback plan JSON |
| `ods validate evidence --file <path>` | Evidence bundle JSON |
| `ods validate release --file <path>` | Release readiness JSON |
| `ods validate approval-policy --file <path>` | Approval policy JSON |

### CI Hallucination Detection (Key Differentiator)

| Command | What it does |
|---|---|
| `ods ci parse --file ci.log --pipeline build-123` | Parse CI log → structured report with AI hallucination detection |
| `ods ci explain --file ci.log --pipeline build-123` | Human-readable explanation of failures with AI attribution |
| `ods ci fix-suggestions --file ci.log --pipeline build-123` | Prioritized fix suggestions for AI-caused failures |

`ods ci` detects patterns unique to AI-generated code:
- **Non-existent symbols** — AI hallucinates functions/classes that don't exist
- **Wrong imports** — AI invents package paths
- **Incorrect defaults** — AI generates plausible but wrong config values
- **Fake URLs** — AI fabricates endpoints

This is currently the only open-source tool that connects CI failure analysis to AI hallucination patterns.

### Template Generation

| Command | What it generates |
|---|---|
| `ods generate branch --type feature --desc "add-oauth"` | Conventional Branch name |
| `ods generate commit --type feat --scope auth --desc "add login" --ai-tool "Claude"` | Conventional Commit with AI disclosure |
| `ods generate pr --ai-tool "Claude"` | PR description template with AI Disclosure |
| `ods review generate --pr 42 --level L2` | AI change review record (L1/L2/L3) |
| `ods review validate --file review.json` | Validate review record against ODS schema |

---

## The 10 Compliance Checks

ODS runs 10 checks across four severity tiers. Each check has a weight that contributes to the 0-100 score.

| # | Check | Weight | Why it matters |
|---|-------|--------|----------------|
| 1 | **AI Disclosure** | 10 | Foundation. Without it, you can't audit AI's safety impact. |
| 2 | **Human Review Evidence** | 10 | 80% of AI PRs get zero human comments. Approval ≠ review. |
| 3 | **Required CI** | 7 | AI code needs the same safety net as human code. |
| 4 | **Approval Policy** | 7 | Policy + evidence = defense in depth. |
| 5 | **AI Agent Commit Detection** | 7 | Agent commits without human review are the highest-risk scenario. |
| 6 | **Test Evidence** | 7 | AI code most commonly lacks tests for edge cases and boundaries. |
| 7 | **Security Scan Evidence** | 7 | 25% of AI code has vulnerabilities. A scan is the minimum defense. |
| 8 | **PR Description** | 5 | Structured descriptions create an audit trail. |
| 9 | **Release Readiness** | 5 | ODS checks should be release gates, not just PR checks. |
| 10 | **Commit Message** | 2 | Structured metadata enables automated AI contribution tracking. |

Full documentation: [docs/checks/README.md](docs/checks/README.md)

---

## How AI Disclosure Works

ODS uses **qualitative AI disclosure**, not percentage estimates. Percentages are brittle, easy to game, and don't help reviewers. Instead:

```markdown
## AI Disclosure
- [x] This PR contains AI-generated code
- **AI Tool:** Claude
- **AI Scope:** OAuth token refresh logic, state validation, unit tests
- **Human Review:** Verified against OAuth 2.0 spec (RFC 6749), checked PKCE flow,
  reviewed error handling for token expiry edge cases
```

This tells a reviewer **exactly what to focus on** — the AI Scope is where they need to look hardest, and the Human Review confirms what was already checked.

---

## Compliance Report

```bash
ods report
```

The `ods report` command discovers your repository context automatically (branch, commit, PR body, CI config, changed files, reviewer data) and produces:

```
ods-report/
├── index.html              Standalone HTML report
├── ods-compliance.json     Machine-readable JSON
├── ods-compliance.svg      Badge for README
├── ods-summary.md          Markdown for CI summaries
└── ods-compliance.sarif    SARIF v2.1.0 for GitHub Code Scanning
```

Output formats via `--format`: terminal (default), json, html, markdown, sarif, files.

Use `--threshold 85` to fail CI if the score drops below a threshold:

```yaml
# In your CI workflow:
- run: ods report --format markdown --threshold 85 >> $GITHUB_STEP_SUMMARY
```

---

## Git Hooks (Instant Feedback)

```bash
ods hook install           # Install all hooks
ods hook install pre-commit  # Pre-commit only
```

Installed hooks catch issues immediately in your terminal:

- **pre-commit** — Validates branch naming
- **commit-msg** — Validates commit message format
- **pre-push** — Quick compliance check before pushing

No more waiting for CI to tell you the branch name is wrong.

---

## Policy Profiles

ODS ships with three profiles. Select yours in `.ods.yaml`:

| Profile | AI Disclosure | Ticket Required | Commit Scope | Use Case |
|---|---|---|---|---|
| `oss` | Optional | No | No | Open-source projects |
| `enterprise` | Required | No | Yes | Teams adopting AI governance |
| `regulated` | Required, strict | Yes | Yes | SOC2, HIPAA, FedRAMP |

Enterprise and regulated profiles escalate AI disclosure to blocking errors.

---

## AI Agent Integration

ODS works with the tools your team already uses:

- **Claude Code** — Reads `AGENTS.md` automatically for ODS instructions
- **Cursor** — Reads `.cursor/rules/ods-compliance.mdc` for context
- **GitHub Copilot** — PR template is automatically applied
- **Pre-commit hooks** — Validates before AI-generated commits land

`ods init` generates all of these files. AI agents become ODS-compliant by default.

---

## Configuration

ODS CLI looks for configuration in:

1. `.ods.yaml` (repository root)
2. `~/.config/ods/config.yaml` (user home)
3. Environment variables (`ODS_*`)

---

## License

[Apache License 2.0](LICENSE)
