# ODS CLI

> **Detect AI-generated code, analyze its quality, and prevent technical debt — before it reaches production.**

[![CI](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml/badge.svg)](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev)
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
2. **Analyze** — What quality issues does the AI code have?
3. **Score** — How much technical debt does this PR add?
4. **Enforce** — Block low-quality AI code from reaching production.

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

### AI Code Detection

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

### AI Code Quality Analysis

| Command | What it does |
|---|---|
| `ods analyze --file <path>` | Analyze a single file for AI code quality issues |
| `ods analyze --dir <path>` | Analyze all code files in a directory |
| `ods analyze` | Analyze git diff against HEAD~1 |
| `ods analyze --ai-only` | Only analyze files detected as AI-generated |
| `ods analyze --json` | Machine-readable JSON output |
| `ods analyze --format detail` | Detailed per-issue report |

**Analysis rules:**

| Rule | What it detects | Severity |
|---|---|---|
| `ai-redundant-error-handling` | Dense clusters of if-err-nil blocks (AI over-defends) | medium |
| `ai-over-commenting` | Comment-to-code ratio >40% (AI hallmark) | medium-high |
| `ai-missing-edge-case` | Multiple if-statements without else branches | low |
| `ai-unsafe-deserialization` | json.Unmarshal into interface{} without type checking | high |
| `ai-inconsistent-pattern` | Mixed naming conventions and indentation styles | medium-low |

### Technical Debt Scoring

| Command | What it does |
|---|---|
| `ods score` | Score technical debt delta for the current diff |
| `ods score --json` | Machine-readable JSON output |
| `ods score --format detail` | Detailed breakdown of all 5 dimensions |

**Scoring dimensions:**

| Dimension | Weight |
|---|---|
| AI code ratio | 3.0 |
| Defect density (issues/KLOC) | 2.0 |
| Critical issues | 1.5 each |
| Test coverage gap | 1.0 |
| Code duplication rate | 1.0 |

Verdict: **decrease** / **neutral** / **increase**

### Enterprise Policy Enforcement

| Command | What it does |
|---|---|
| `ods check` | Evaluate OPA Rego policy against the current change |
| `ods check --policy <path>` | Use a custom Rego policy file |
| `ods check --json` | Machine-readable JSON output |

Place your policy at `.ods/policy.rego`:

```rego
package ods.policy

default allow := true

deny[msg] {
    input.ai_confidence > 0.8
    input.test_coverage < 0.3
    msg = "AI code with low test coverage"
}
```

### Git Hooks

| Command | What it does |
|---|---|
| `ods hook install` | Install pre-commit, prepare-commit-msg, pre-push hooks |
| `ods init` | Scaffold CI workflow, AGENTS.md, Cursor rules |

---

## Detection Examples

```bash
# High-confidence detection via PR body
$ ods detect --pr-body "$(cat pr.md)"
🤖  AI code detected (confidence: 85%)
   Sources: pr-body
   Evidence:
     • [pr-body] AI disclosure checkbox is checked (85%)

# Branch-level detection
$ ods detect --branch feature/ai-oauth
🤖  AI code detected (confidence: 35%)
   Sources: branch-name
   Evidence:
     • [branch-name] Branch 'feature/ai-oauth' has AI-prefixed segment (35%)

# No AI detected
$ ods detect --branch feature/add-login
👤  No AI code detected (confidence: 0%)
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

## License

[Apache License 2.0](LICENSE)
