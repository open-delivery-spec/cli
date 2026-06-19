# ODS CLI

> **Detect AI-generated code, analyze its quality, and prevent technical debt — before it reaches production.**

[![CI](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml/badge.svg)](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

---

## The Problem

A PR arrives: 8 commits, 6 written by Copilot, 2 by a human. The branch says `feature/add-sarif-output`. But two changed files touch the authentication module — nothing to do with SARIF. The reviewer doesn't know. The merge happens. A bug ships.

This is the new reality of AI-assisted development. AI code increases technical debt in predictable ways:

| AI Failure Mode | Real-world impact |
|---|---|
| **Hallucinated APIs** | AI invents functions, packages, and endpoints that don't exist |
| **Redundant error handling** | AI over-defends: 3+ identical `if err != nil` blocks in the same function |
| **Over-commenting** | AI writes 35%+ comment-to-code ratio with self-explanatory comments |
| **No test coverage** | AI PRs average 22% test coverage vs 68% for human PRs |
| **Invisible AI code** | Teams can't distinguish AI-generated from human-written changes |
| **Scope drift** | AI changes files unrelated to the stated feature |

**ODS is the CI gate that detects AI code, analyzes its quality, scores technical debt impact, and enforces enterprise policy — on every pull request.**

---

## Quick Start

```bash
# Install
go install github.com/open-delivery-spec/cli/cmd/ods@latest

# Detect AI code in a PR
ods detect --diff-base origin/main --branch feature/my-feature

# Analyze code quality
ods analyze --json

# Score technical debt
ods score

# Enforce policy
ods check
```

---

## Command Reference with Real Output

### `ods detect` — AI Code Detection

Detects AI-generated code using commit trailers, branch names, PR disclosure, and diff heuristics.

```bash
$ ods detect --diff-base origin/main --branch feature/ai-oauth
🤖  AI code detected — 85% confidence (PR shows AI disclosure)
   Sources: pr-body
   Evidence:
     • [pr-body] AI disclosure checkbox is checked (85%)
```

```bash
$ ods detect --diff-base origin/main --branch feature/add-login
👤  No AI code detected (0% confidence)
```

JSON output for CI pipelines:

```bash
$ ods detect --diff-base origin/main --json
{
  "ai_generated": true,
  "confidence": 0.85,
  "summary": "AI code detected — 85% confidence (PR shows AI disclosure)",
  "sources": ["pr-body"],
  "evidence": [
    {
      "source": "pr-body",
      "value": "AI disclosure checkbox is checked",
      "confidence": 0.85
    }
  ],
  "files": [
    {
      "path": "internal/scanner/sarif.go",
      "ai_lines": 180,
      "total_lines": 195,
      "confidence": 0.92
    }
  ]
}
```

| Flag | Default | Description |
|------|---------|-------------|
| `--diff-base` | `HEAD~1` | Git ref to diff against |
| `--branch` | auto | Branch name |
| `--pr-body` | — | PR description body text |
| `--pr-file` | — | File containing PR body |
| `--commits` | `10` | Max commits to scan |
| `--json` | `false` | JSON output |
| `--format` | `summary` | Output format: `summary`, `detail`, `json` |

### `ods analyze` — AI Code Quality Analysis

```bash
$ ods analyze --file internal/scanner/sarif.go --json
{
  "issues": [
    {
      "file": "internal/scanner/sarif.go",
      "line": 42,
      "rule": "ai-over-commenting",
      "severity": "medium",
      "message": "Comment-to-code ratio is 47% — exceeds 40% threshold",
      "suggestion": "Remove self-explanatory inline comments; keep only public API docstrings"
    },
    {
      "file": "internal/scanner/sarif.go",
      "line": 88,
      "rule": "ai-inconsistent-pattern",
      "severity": "low",
      "message": "Mixed error wrapping patterns in the same function",
      "suggestion": "Standardize on fmt.Errorf for all error wrapping"
    }
  ],
  "total_lines": 195,
  "summary": "2 quality issues found (0 critical, 0 high, 1 medium, 1 low)"
}
```

| Flag | Default | Description |
|------|---------|-------------|
| `--file`, `-f` | — | Analyze a single file |
| `--dir`, `-d` | — | Analyze a directory (recursively) |
| `--ai-only` | `false` | Only files detected as AI-generated |
| `--json` | `false` | JSON output |
| `--format` | `summary` | Output format: `summary`, `detail`, `json` |

### `ods score` — Technical Debt Impact

```bash
$ ods score
⚠️  Technical Debt Score
   +4.2 (increase)
   Verdict: increase (Moderate risk: review recommended, ensure adequate tests)
```

```bash
$ ods score --json
{
  "technical_debt_delta": 4.2,
  "verdict": "increase",
  "recommendation": "Moderate risk: review recommended, ensure adequate tests",
  "breakdown": {
    "ai_code_ratio": 0.75,
    "defect_density": 1.2,
    "critical_issues": 0,
    "test_coverage": 0.3,
    "duplication_rate": 0.1
  }
}
```

| Flag | Default | Description |
|------|---------|-------------|
| `--json` | `false` | JSON output |
| `--format` | `summary` | Output format: `summary`, `detail`, `json` |
| `--test-dir` | — | Test directory path (auto-detected) |

### `ods check` — Enterprise Policy Enforcement

```bash
$ ods check
✅  Policy check passed
   Policy: .ods/policy.rego
```

```bash
$ ods check --json
{
  "allowed": false,
  "denials": ["AI code with low test coverage"],
  "warnings": ["High-confidence AI code with multiple quality issues"]
}
```

| Flag | Default | Description |
|------|---------|-------------|
| `--policy`, `-p` | `.ods/policy.rego` | Path to Rego policy file |
| `--json` | `false` | JSON output |

### `ods hook install` — Git Hooks

```bash
$ ods hook install
✅  pre-commit hook installed at .git/hooks/pre-commit
✅  prepare-commit-msg hook installed at .git/hooks/prepare-commit-msg
✅  pre-push hook installed at .git/hooks/pre-push
```

### `ods init` — Project Scaffolding

```bash
$ ods init
✅  Created .github/workflows/ods-ai-quality.yml
✅  Created AGENTS.md (agent instructions for AI coding assistants)
✅  Created .cursor/rules/gov-001-ods-compliance.mdc (Cursor rules)
✅  Created .ods/policy.rego (default enterprise policy)
```

---

## Installation and CI Integration

### Install

```bash
go install github.com/open-delivery-spec/cli/cmd/ods@latest
```

Requires Go 1.25+.

### GitHub Actions (Recommended)

Use the one-step [validate-action](https://github.com/open-delivery-spec/validate-action):

```yaml
name: ODS AI Code Quality
on:
  pull_request:
    types: [opened, synchronize, reopened]

jobs:
  ods:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0
      - uses: open-delivery-spec/validate-action@v2
```

Or use individual CLI steps:

```yaml
- name: Detect AI code
  run: ods detect --diff-base origin/main --branch ${{ github.head_ref }} --json

- name: Analyze quality
  run: ods analyze --json

- name: Score tech debt
  run: ods score --json

- name: Enforce policy
  run: ods check --json
```

### See Also

- [Module 04 End-to-End Example](https://github.com/open-delivery-spec/spec/tree/main/examples/module-04-ai-change-review) — Realistic L1/L2/L3 review scenario with scope drift detection
- [validate-action README](https://github.com/open-delivery-spec/validate-action) — GitHub Action documentation
- [Spec documentation](https://github.com/open-delivery-spec/spec) — Full specification and design principles

---

## License

[Apache License 2.0](LICENSE)
