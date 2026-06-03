# ODS CLI

**Open Delivery Spec — AI code delivery compliance framework.**

[![CI](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml/badge.svg)](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml)
[![ODS L1](https://img.shields.io/badge/ODS-L1%20Structured%20Delivery-blue)](https://github.com/open-delivery-spec/spec)
[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

> **Dogfooding:** This repository uses ODS to validate its own PRs.

## What is ODS?

ODS is an **AI-generated code delivery compliance framework**. It checks for the unique risks that AI code introduces:

- **Review fatigue:** 80% of PRs with AI tools have zero human comments
- **Identity ambiguity:** Who wrote this — human or AI agent?
- **Hallucination in production:** AI-invented APIs, packages, configs
- **Security blind spots:** 25% of AI code has confirmed vulnerabilities
- **Test vacuum:** AI code works but lacks edge cases and boundaries

## Quick Start

```bash
# Install
go install github.com/open-delivery-spec/cli/cmd/ods@latest

# Init in your repo
ods init

# Scan your project (zero setup)
ods report

# See what each check means
ods checks list
ods checks explain ai-disclosure

# Get fix suggestions
ods fix
```

## Command Reference

| Command | Purpose | Status |
|---------|---------|--------|
| `ods init` | Scaffold ODS config in a repo | ✅ Production |
| `ods report` | Generate compliance report (10 checks, weighted scoring) | ✅ Production |
| `ods checks list` | List all 10 compliance checks | ✅ Production |
| `ods checks explain <id>` | Detailed check documentation | ✅ Production |
| `ods fix` | Generate and apply fix suggestions | ✅ Production |
| `ods badge` | Generate shields.io JSON for dynamic badges | ✅ Production |
| `ods validate branch\|commit\|pr` | Validate individual artifacts | ✅ Production |
| `ods validate rollback\|evidence\|release` | Validate ODS JSON schemas | ✅ Production |

> [!NOTE]
> Other command groups (`generate`, `release`, `evidence`, `ci`, `review`, `approval`) are **experimental** — they exist as direction-setting placeholders for future modules 04-09 and may produce placeholder output. See [Roadmap](https://github.com/open-delivery-spec/spec/blob/main/ROADMAP.md) for module maturity.

## Dynamic Badge

Add a live compliance badge to your README:

```markdown
[![ODS](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/USER/REPO/main/ods-report/ods-badge.json)](...)
```

Generate the badge JSON with:

```bash
ods badge > ods-report/ods-badge.json
git add ods-report/ && git commit -m "Update ODS badge"
```

## Checks

ODS runs 10 compliance checks across four severity tiers:

| # | Check | Weight | Category |
|---|-------|--------|----------|
| 1 | AI Disclosure | 10 | Critical |
| 2 | Human Review Evidence | 10 | Critical |
| 3 | Required CI | 7 | High |
| 4 | Approval Policy | 7 | High |
| 5 | AI Agent Commit Detection | 7 | High |
| 6 | Test Evidence | 7 | High |
| 7 | Security Scan Evidence | 7 | High |
| 8 | PR Description | 5 | Medium |
| 9 | Release Readiness | 5 | Medium |
| 10 | Commit Message | 2 | Low |

Full documentation: [docs/checks/README.md](docs/checks/README.md)

## Install

```bash
go install github.com/open-delivery-spec/cli/cmd/ods@latest
```

or download from [Releases](https://github.com/open-delivery-spec/cli/releases).

## Quick Start

```bash
# One-command scaffold for a new repo
ods init github

# Validate a branch name
ods validate branch feature/add-oauth-login

# Validate a commit message (from file or stdin)
ods validate commit --file commit-msg.txt

# Validate a PR description
ods validate pr --file PR_BODY.md

# Generate a compliance report (HTML, JSON, SVG, Markdown, SARIF)
ods report

# Strict mode — treat warnings as errors
ods validate branch feat/AI-experiment --strict
```

## Stable M1 Commands

### `ods validate`

Validate the L1 delivery artifacts that are ready for CI enforcement.

```bash
ods validate branch <name>              # Validate branch name
ods validate commit [--file | --stdin]  # Validate commit message
ods validate pr [--file | --stdin]      # Validate PR description
```

All stable validate subcommands support `--strict` to treat warnings as errors.

## Compliance Report

Generate an ODS L1 compliance report with convention-first defaults:

```bash
ods report
```

The command writes `ods-report/` by default:

```text
ods-report/
├── index.html              (standalone HTML report)
├── ods-compliance.json     (machine-readable JSON)
├── ods-compliance.svg      (badge for README)
├── ods-summary.md          (Markdown for CI summaries)
└── ods-compliance.sarif    (SARIF v2.1.0 for code scanning)
```

`ods report` reads GitHub Actions context when available and falls back to local git metadata. PR-only data, such as the PR description, is skipped when it is not available.

Use `--output` only when you need a different report directory:

```bash
ods report --output build/ods-report
```

## Draft Schema Validation

These commands validate JSON files against draft module expectations. They are useful for experimentation, but the corresponding workflows are not production gates yet.

```bash
ods validate rollback [--file | --stdin]         # Validate rollback plan JSON
ods validate evidence [--file | --stdin]         # Validate evidence bundle JSON
ods validate release [--file | --stdin]          # Validate release readiness JSON
ods validate approval-policy [--file | --stdin]  # Validate approval policy JSON
ods review validate [--file | --stdin]           # Validate AI review JSON
```

## Candidate M2 Commands

### `ods review`

Generate and validate AI change review records with L1/L2/L3 level support.

```bash
# Generate L2 review record
ods review generate --pr 42 --level L2 --ai-pct 45

# Generate L3 review record (auto-detected from high AI percentage)
ods review generate --pr 99 --level L3 --ai-pct 92

# Validate a review record
ods review validate --file review.json

# Estimate AI contribution from commit log
ods review ai-percentage --pr 42
```

### `ods ci`

Parse CI failure logs and produce structured reports with AI hallucination detection.

```bash
# Parse CI log with hallucination detection
ods ci parse --file ci-output.log --pipeline build-12345 --repo org/my-service

# Explain failures in human-readable form
ods ci explain --file ci-output.log --pipeline build-12345

# Get prioritized fix suggestions
ods ci fix-suggestions --file ci-output.log --pipeline build-12345
```

## Experimental Command Groups

The following command groups are registered but currently include placeholder output. They will gain real functionality as their corresponding spec modules mature.

### `ods generate`
```
ods generate branch --type feature --description "add-oauth"
ods generate commit --type feat --scope auth
ods generate pr
ods generate release --version v1.4.0
ods generate rollback --version v1.4.0 --strategy feature_flag
```

### `ods release`
```
ods release check --version v1.4.0
```

### `ods evidence`
```
ods evidence generate --release v1.4.0 --env production
ods evidence verify <bundle-file>
ods evidence audit
```

### `ods approval`
```
ods approval validate-policy --file policy.json
ods approval check --pr 42
```

## Configuration

ODS CLI looks for configuration in:

1. `.ods.yaml` (repository root)
2. `~/.config/ods/config.yaml` (user home)
3. Environment variables (`ODS_*`)

```yaml
# .ods.yaml
schemas:
  spec_version: "1.0.0"
  schema_base_url: "https://open-delivery-spec.dev/schemas"

policies:
  approval: "ods-approval.json"

ci:
  provider: github-actions
```

## Schema Validation

All schemas are defined as [JSON Schema Draft 2020-12](https://json-schema.org/draft/2020-12) in the [spec](https://github.com/open-delivery-spec/spec) repository. The CLI bundles embedded copies and validates artifacts against these specification rules.

## License

[Apache License 2.0](LICENSE)
