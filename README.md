# ODS CLI

> **Zero-config AI code quality gate for teams using Claude Code, Copilot, or Cursor.** These tools already stamp `Co-Authored-By` trailers on every commit, so ODS attributes AI-generated code automatically in CI — then analyzes quality, scores technical debt, and enforces policy on every PR. No disclosure forms, no manual tagging.

[![CI](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml/badge.svg)](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev)
[![GitHub License](https://img.shields.io/github/license/open-delivery-spec/cli?logo=apache)](LICENSE)

---

## The Problem

A PR arrives: 8 commits, 6 written by Copilot, 2 by a human. The branch says `feature/add-sarif-output`. But two changed files touch the authentication module — nothing to do with SARIF. The reviewer doesn’t know. The merge happens. A bug ships.

This is the new reality of AI-assisted development. AI code increases technical debt in predictable ways:

| AI Failure Mode | Real-world impact |
|---|---|
| **Hallucinated APIs** | AI invents functions, packages, and endpoints that don’t exist |
| **Redundant error handling** | AI over-defends: 3+ identical `if err != nil` blocks in the same function |
| **Over-commenting** | AI writes 35%+ comment-to-code ratio with self-explanatory comments |
| **No test coverage** | AI PRs average 22% test coverage vs 68% for human PRs |
| **Invisible AI code** | Teams can’t distinguish AI-generated from human-written changes |
| **Scope drift** | AI changes files unrelated to the stated feature |

**ODS is the CI gate that detects AI code, analyzes its quality, scores technical debt impact, and enforces enterprise policy — on every pull request.**

---

## In Production

ODS runs on every PR in the `open-delivery-spec` org (dogfooding):

[![ODS on spec](https://github.com/open-delivery-spec/spec/actions/workflows/ods-validate.yml/badge.svg)](https://github.com/open-delivery-spec/spec/actions/workflows/ods-validate.yml)
[![ODS on cli](https://github.com/open-delivery-spec/cli/actions/workflows/ods-validate.yml/badge.svg)](https://github.com/open-delivery-spec/cli/actions/workflows/ods-validate.yml)
[![ODS on validate-action](https://github.com/open-delivery-spec/validate-action/actions/workflows/ods-validate.yml/badge.svg)](https://github.com/open-delivery-spec/validate-action/actions/workflows/ods-validate.yml)

See [ADOPTERS.md](https://github.com/open-delivery-spec/spec/blob/main/ADOPTERS.md) for the full list and pending external adoption.

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

### `ods detect` — AI Code Attribution

Attributes AI-generated code using `Co-Authored-By` trailers, commit `AI-assisted:` footers, branch names, PR disclosure, and diff heuristics.

`Co-Authored-By` trailers emitted by Claude Code, GitHub Copilot, and Cursor are the **primary** signal — no additional configuration needed. This is attribution from signals the tools volunteer, not forensic detection: stripping the trailer evades it, and the diff heuristics are only a low-confidence fallback.

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
      "severity": "info",
      "message": "Comment-to-code ratio is 47%",
      "suggestion": "If comments restate the code, prefer explaining why over what; documentation comments are fine"
    },
    {
      "file": "internal/scanner/sarif.go",
      "line": 88,
      "rule": "ai-inconsistent-pattern",
      "severity": "medium",
      "message": "Mixed naming conventions in the same file",
      "suggestion": "Standardize on one naming convention; run gofmt/prettier"
    }
  ],
  "total_lines": 195,
  "summary": "2 quality issues found (0 critical, 0 high, 1 medium, 0 low, 1 info)"
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
| `--sarif` | — | SARIF file whose findings are merged into the score |

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
| `--sarif` | — | SARIF file whose findings are merged into the policy input |

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
  ✅ Created: .github/workflows/ods-ai-quality.yml
  ✅ Created: .ods/policy.rego

── ODS initialized ──

Next steps:
  1. Edit .ods/policy.rego to add custom enforcement rules
  2. Install git hooks:  ods hook install
  3. Commit and push — ODS will run on your next PR
```

`init` is idempotent — existing files are skipped, never overwritten.

### `ods rules` — Rule Catalogue

```bash
$ ods rules
ODS Analysis Rules (4)

🔴 [high] ai-unsafe-deserialization
  json.Unmarshal into interface{} without type validation — AI commonly skips type checking.
  → Use a concrete struct type or validate the unmarshalled data before use.
...
```

`ods rules --json` emits the machine-readable catalogue (id, name, description,
default severity, category, suggestion). It is the single source of truth for
every rule the analyzer can emit.

### `ods report` — AI Attribution Report

A governance view over recent history: how much delivered work is AI-assisted,
and trending which way. Attribution comes from the `Co-Authored-By` trailers AI
tools emit automatically — the same signal as `ods detect`.

```bash
$ ods report --since "90 days ago"
ODS AI Attribution Report — since 90 days ago

  Commits:        64 total · 5 AI-assisted (8%) · 59 human
  Changed lines:  30056 total · 1697 AI-assisted (6%)

  By tool:
    Claude               3 commit(s)
    Claude Sonnet 4.6    2 commit(s)

64 commit(s): 5 AI-assisted (8%), 59 human — AI touched 6% of changed lines
```

| Flag | Default | Description |
|------|---------|-------------|
| `--since` | `90 days ago` | History window (any git `--since` expression) |
| `--max-commits` | `0` | Cap commits scanned (0 = no cap) |
| `--json` | `false` | Machine-readable output (commit/line shares, per-tool counts) |

This is attribution, not forensic detection: it counts what the tools disclose.
Coverage/quality history is not reconstructable from git alone, so the report
focuses on the signals git carries reliably — AI share of commits and churn.

---

## Debugging

Every command accepts a global `--debug` flag (or set `ODS_DEBUG=1`) to print
decision diagnostics to **stderr**. JSON written to stdout stays clean, so
`--debug` is safe to combine with `--json` in pipelines.

```bash
$ ods check --json --debug
[ods:debug] check: diff base = HEAD~1
[ods:debug] check: no policy file found, using built-in default policy
[ods:debug] check: detection ai_generated=true confidence=0.90 sources=[commit-trailer]
[ods:debug] check: analysis issues=0 (changed lines=2, test lines=0)
[ods:debug] check: coverage source=unknown value=-1.00
[ods:debug] check: score delta=0.00 verdict=decrease (ai_ratio=0.00 ...)
[ods:debug] check: policy result allowed=true denials=0 warnings=0
{
  "allowed": true
}
```

This answers "why did this PR pass/block?" by exposing the detection signals,
score breakdown, coverage source, and every policy denial/warning.

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
      - uses: open-delivery-spec/validate-action@v1
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

- [validate-action README](https://github.com/open-delivery-spec/validate-action) — GitHub Action documentation
- [Spec documentation](https://github.com/open-delivery-spec/spec) — Full specification and design principles
- [ADOPTERS.md](https://github.com/open-delivery-spec/spec/blob/main/ADOPTERS.md) — Who is using ODS

---

## License

[Apache License 2.0](LICENSE)
