# ODS Validate Action

Official GitHub Action for [Open Delivery Spec](https://open-delivery-spec.github.io/spec) compliance checks.

This action runs all 10 ODS compliance checks against your repository and:
- Uploads results as SARIF to GitHub Code Scanning
- Writes a markdown summary to the workflow run
- Comments on pull requests with compliance results
- Fails the workflow if the score falls below a configurable threshold

## Usage

### Basic PR Check

```yaml
name: ODS Compliance
on:
  pull_request:
    types: [opened, synchronize, reopened]

jobs:
  ods:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Run ODS Check
        uses: open-delivery-spec/validate-action@v1
        with:
          branch_name: ${{ github.head_ref }}
          commit_message: ${{ github.event.head_commit.message }}
          pr_body: ${{ github.event.pull_request.body }}
```

### With Threshold

```yaml
- uses: open-delivery-spec/validate-action@v1
  with:
    threshold: '85'
    profile: 'enterprise'
```

### Selective Checks

```yaml
- uses: open-delivery-spec/validate-action@v1
  with:
    check: 'ai-disclosure,human-review-evidence,required-ci'
```

## Inputs

| Input | Description | Default |
|-------|-------------|---------|
| `check` | Checks to run (comma-separated, or "all") | `all` |
| `branch_name` | Branch name | Auto-detected |
| `commit_message` | Commit message | Auto-detected |
| `pr_body` | PR description body | Auto-detected |
| `strict` | Treat warnings as errors | `false` |
| `threshold` | Fail if score below this value | `0` |
| `format` | Output format: sarif, json, markdown | `sarif` |
| `policy` | Path to policy file | Auto-detected |
| `profile` | Policy profile: oss, enterprise, regulated | `enterprise` |
| `upload_sarif` | Upload results to Code Scanning | `true` |

## Outputs

| Output | Description |
|--------|-------------|
| `status` | Compliance status |
| `score` | Score (0-100) |
| `sarif_file` | Path to SARIF output |

## Checks

| Check | Weight | Category |
|-------|--------|----------|
| AI Disclosure | 10 | Critical |
| Human Review Evidence | 10 | Critical |
| Required CI | 7 | High |
| Approval Policy | 7 | High |
| AI Agent Commit Detection | 7 | High |
| Test Evidence | 7 | High |
| Security Scan Evidence | 7 | High |
| PR Description | 5 | Medium |
| Release Readiness | 5 | Medium |
| Commit Message | 2 | Low |
