# ODS CLI

**Reference CLI tool for Open Delivery Spec validation and generation.**

[![CI](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml/badge.svg)](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

> **Status**: Early development. `validate` subcommands are functional. Other command groups (`generate`, `review`, `release`, `evidence`, `ci`, `approval`) are stubs that print placeholder output. See [Roadmap](https://github.com/open-delivery-spec/spec/blob/main/ROADMAP.md) for module maturity.

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

## Stable Commands

### `ods validate`

Validate delivery artifacts against ODS schemas.

```bash
ods validate branch <name>         # Validate branch name
ods validate commit [--file | --stdin]  # Validate commit message
ods validate pr [--file | --stdin]      # Validate PR description
ods validate rollback [--file | --stdin]# Validate rollback plan
ods validate evidence [--file | --stdin]# Validate evidence bundle
ods validate release [--file | --stdin]# Validate release readiness report
ods validate approval-policy [--file]   # Validate approval policy
```

All validate subcommands support `--strict` to treat warnings as errors.

## Experimental Commands

The following command groups are registered but currently print placeholder output. They will gain real functionality as their corresponding spec modules mature.

### `ods generate`
```
ods generate branch --type feature --description "add-oauth"
ods generate commit --type feat --scope auth
ods generate pr
ods generate release --version v1.4.0
ods generate rollback --version v1.4.0 --strategy feature_flag
```

### `ods review`
```
ods review generate --pr 42
ods review validate
ods review ai-percentage --pr 42
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

### `ods ci`
```
ods ci parse --file ci-output.log
ods ci explain <pipeline-id>
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
