# ODS CLI

**Reference CLI tool for Open Delivery Spec validation.**

[![CI](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml/badge.svg)](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

> [!NOTE]  
> **Status**: Early development. The production-ready M1 surface is `ods validate branch`, `ods validate commit`, and `ods validate pr`. Other validation commands are schema checks for draft modules. Command groups such as `generate`, `release`, `evidence`, `ci`, and `approval` are experimental and may print placeholder output. See [Roadmap](https://github.com/open-delivery-spec/spec/blob/main/ROADMAP.md) for module maturity.

## Install

```bash
go install github.com/open-delivery-spec/cli/cmd/ods@latest
```

or download from [Releases](https://github.com/open-delivery-spec/cli/releases).

## Quick Start

```bash
# Validate a branch name
ods validate branch feature/add-oauth-login

# Validate a commit message (from file or stdin)
ods validate commit --file commit-msg.txt

# Validate a PR description
ods validate pr --file PR_BODY.md

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
