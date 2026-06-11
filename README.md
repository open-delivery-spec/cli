# ODS CLI

> **Detect AI-generated code, analyze its quality, and prevent technical debt — before it reaches production.**

[![CI](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml/badge.svg)](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

---

## The Problem

Enterprises are adopting AI coding tools (Copilot, Cursor, Claude Code) at speed — but AI-generated code increases technical debt in predictable ways:

| AI Failure Mode | Real-world impact |
|---|---|
| **Hallucinated APIs** | AI invents functions, packages, and endpoints that don't exist |
| **Redundant error handling** | AI over-defends: 3+ identical `if err != nil` blocks in the same function |
| **Over-commenting** | AI writes 35%+ comment-to-code ratio with self-explanatory comments |
| **No test coverage** | AI PRs average 22% test coverage vs 68% for human PRs |
| **Invisible AI code** | Teams can't distinguish AI-generated from human-written changes |

**ODS is the CI gate that detects AI code, analyzes its quality, and blocks low-quality AI changes before they create technical debt.**

---

## What ODS Does

1. **Detect** — Which code is AI-generated? (without relying on developer self-disclosure)
2. **Analyze** — What quality issues does the AI code have? (coming in v2)
3. **Score** — How much technical debt does this PR add? (coming in v2)
4. **Enforce** — Block low-quality AI code from reaching production. (coming in v2)

---

## Quick Start

```bash
# Install
go install github.com/open-delivery-spec/cli/cmd/ods@latest

# Detect AI code in the current branch
ods detect

# Detect against a specific base
ods detect --diff-base origin/main

# Detect with explicit PR context
ods detect --branch feature/ai-auth --pr-body "$(cat PR_BODY.md)"

# JSON output for CI pipelines
ods detect --json
```

---

## Command Reference

### AI Code Detection (primary)

| Command | What it does |
|---|---|
| `ods detect` | Detect AI-generated code using commit trailers, branch names, PR disclosure, and diff heuristics |
| `ods detect --json` | Machine-readable JSON output for CI pipelines |
| `ods detect --format detail` | Detailed file-level AI detection report |
| `ods detect --diff-base origin/main` | Detect AI code in PR diff against main |
| `ods detect --pr-body "..."` | Include PR body in detection |
| `ods detect --commits 20` | Scan last 20 commits for AI markers |

**Detection signals (in order of confidence):**
1. Git commit trailers (`AI-assisted: true`, `AI-tool: name`)
2. PR description AI disclosure checkbox/section
3. Branch name prefix (`ai-*`)
4. Code diff heuristics (comment ratio, verbose naming, redundant error handling, uniform indentation)

### Delivery Governance (legacy)

| Command | What it does |
|---|---|
| `ods validate branch <name>` | Validate branch naming |
| `ods validate commit --file <path>` | Validate commit message format |
| `ods validate pr --file <path>` | Validate PR description structure |
| `ods report` | Generate compliance report (terminal, JSON, HTML, SARIF, Markdown) |
| `ods init` | Scaffold ODS config, PR template, CI workflows |
| `ods hook install` | Install pre-commit, commit-msg hooks |
| `ods ci parse --file ci.log` | Parse CI failures for AI hallucination patterns |
| `ods review generate --pr <n> --level L2` | Generate AI change review record |

---

## Detection Examples

### High-confidence detection (PR body + commit trailer)

```bash
$ ods detect --pr-body "$(cat pr.md)"
🤖  AI code detected (confidence: 85%)
   Sources: pr-body
   Evidence:
     • [pr-body] AI disclosure checkbox is checked (85%)
```

### Branch-level detection only

```bash
$ ods detect --branch feature/ai-oauth
🤖  AI code detected in 0 file(s) — 0/0 lines (confidence: 50%)
   Sources: branch-name
   Evidence:
     • [branch-name] Branch 'feature/ai-oauth' has AI-prefixed segment (35%)
```

### No AI detected

```bash
$ ods detect --branch feature/add-login
👤  No AI code detected (confidence: 0%)
```

### CI integration (block AI code in CI)

```yaml
# .github/workflows/ods.yml
- name: Detect AI code
  run: ods detect --json
  # Exits non-zero if AI code detected with ≥80% confidence
```

---

## How Detection Works

ODS does **not** rely on developer self-disclosure. It uses multiple independent signal sources:

| Signal | Source | Confidence |
|---|---|---|
| Commit trailers | `git log` parsing for `AI-assisted: true`, `AI-tool:` fields | 90% |
| PR body | AI Disclosure checkbox/section in PR description | 85% |
| Branch prefix | `ai-*` branch naming convention | 35-50% |
| Diff heuristics | Comment-to-code ratio >35%, verbose variable names, redundant error handling, uniform indentation | 40% |

The weighted combination of these signals produces the final confidence score.

---

## Configuration

ODS CLI looks for configuration in:

1. `.ods.yaml` (repository root)
2. `~/.config/ods/config.yaml` (user home)
3. Environment variables (`ODS_*`)

---

## License

[Apache License 2.0](LICENSE)
