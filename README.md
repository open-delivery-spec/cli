# ODS CLI

**Reference CLI tool for Open Delivery Spec validation and generation.**

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

## Install

```bash
go install github.com/open-delivery-spec/cli/cmd/ods@latest
```

or download from [Releases](https://github.com/open-delivery-spec/cli/releases).

## Quick Start

```bash
# Validate a branch name
ods validate branch feature/add-oauth-login

# Validate a commit message
ods validate commit --file commit-msg.txt

# Validate a PR description
ods validate pr --file PR_BODY.md

# Generate a release readiness report
ods release readiness --version v1.4.0

# Generate production release evidence
ods evidence generate --release v1.4.0 --env production
```

## Commands

### `ods validate`

Validate artifacts against ODS schemas.

```bash
ods validate branch <name>         # Validate branch name
ods validate commit <ref>          # Validate commit message
ods validate pr --file <path>      # Validate PR description
ods validate release --file <path> # Validate release readiness report
ods validate rollback --file <path># Validate rollback plan
ods validate evidence --file <path># Validate evidence bundle
```

### `ods generate`

Generate ODS-compliant templates.

```bash
ods generate branch --type feature --description "add-oauth"
ods generate commit --type feat --scope auth --ai-tool "GitHub Copilot"
ods generate pr --type feature --ai-tool "GitHub Copilot"
ods generate release --version v1.4.0
ods generate rollback --version v1.4.0 --strategy feature_flag
```

### `ods review`

AI change review workflow.

```bash
ods review generate --pr 42
ods review validate --file review-42.json
ods review ai-percentage --pr 42
```

### `ods release`

Release readiness.

```bash
ods release readiness --version v1.4.0
ods release check --version v1.4.0
```

### `ods evidence`

Production release evidence.

```bash
ods evidence generate --release v1.4.0 --env production
ods evidence verify --bundle evidence-v1.4.0.json
ods evidence audit --bundle evidence-v1.4.0.json --framework SOC2
```

### `ods ci`

CI failure analysis.

```bash
ods ci parse --file ci-output.log --format json
ods ci explain --pipeline build-12345
```

### `ods approval`

Approval workflow.

```bash
ods approval validate-policy --file policy.json
ods approval check --pr 42 --policy policy.json
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

All schemas are validated against [JSON Schema Draft 2020-12](https://json-schema.org/draft/2020-12). The CLI bundles schemas from the [spec](https://github.com/open-delivery-spec/spec) repository.

## License

[Apache License 2.0](LICENSE)
