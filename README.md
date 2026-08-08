# ODS CLI

> **Zero-config governance and visibility for AI-assisted code — on every pull request.** Claude Code, Copilot, and Cursor already stamp `Co-Authored-By` trailers on every commit, so ODS shows how much of your delivery is AI-assisted, routes review attention to the changes that need it, and enforces your policy in CI — no disclosure forms, no manual tagging. It governs the AI you can see; it's a signal producer, not a quality oracle.

[![CI](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml/badge.svg)](https://github.com/open-delivery-spec/cli/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-Apache_2.0-green?logo=apache)](LICENSE)

---

## The Problem

A PR arrives: 8 commits, 6 written by Copilot, 2 by a human. The branch says `feature/add-sarif-output`. But two changed files touch the authentication module — nothing to do with SARIF. The reviewer doesn’t know. The merge happens. A bug ships.

This is the new reality of AI-assisted development. AI code increases technical debt in predictable ways:

| AI Failure Mode | Real-world impact |
|---|---|
| **Hallucinated APIs** | AI invents functions, packages, and endpoints that don’t exist |
| **Redundant error handling** | AI over-defends: 3+ identical `if err != nil` blocks in the same function |
| **Over-commenting** | AI writes 35%+ comment-to-code ratio with self-explanatory comments |
| **No test coverage** | AI-generated PRs often ship with little or no accompanying tests |
| **Invisible AI code** | Teams can’t distinguish AI-generated from human-written changes |
| **Scope drift** | AI changes files unrelated to the stated feature |

**ODS is the CI layer that attributes AI-assisted code, surfaces how much of your delivery it is, routes review attention to the risky changes, and enforces your policy — on every pull request.**

---

## In Production

ODS runs on every PR in the `open-delivery-spec` org (dogfooding):

[![ODS on spec](https://github.com/open-delivery-spec/spec/actions/workflows/ods-validate.yml/badge.svg)](https://github.com/open-delivery-spec/spec/actions/workflows/ods-validate.yml)
[![ODS on cli](https://github.com/open-delivery-spec/cli/actions/workflows/ods-validate.yml/badge.svg)](https://github.com/open-delivery-spec/cli/actions/workflows/ods-validate.yml)
[![ODS on validate-action](https://github.com/open-delivery-spec/validate-action/actions/workflows/ods-validate.yml/badge.svg)](https://github.com/open-delivery-spec/validate-action/actions/workflows/ods-validate.yml)

External repositories including [devops-maturity](https://github.com/devops-maturity/devops-maturity) and [conventional-branch](https://github.com/conventional-branch/conventional-branch) run it on every PR as well — see [ADOPTERS.md](https://github.com/open-delivery-spec/spec/blob/main/ADOPTERS.md) for the full list.

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

Attributes AI-generated code using `Co-Authored-By` trailers, the Linux kernel's `Assisted-by:` trailers, commit `AI-assisted:` footers, branch names, PR disclosure, and diff heuristics.

`Co-Authored-By` trailers emitted by Claude Code, GitHub Copilot, and Cursor are the **primary** signal — no additional configuration needed. The [Linux kernel coding-assistants convention](https://docs.kernel.org/process/coding-assistants.html) is recognized as a first-class disclosure with the same confidence:

```text
Assisted-by: Claude:claude-3-opus coccinelle sparse
```

parses the agent (`Claude`) and model version (`claude-3-opus`) into the evidence; the trailing analysis-tool list is not attribution and is ignored. A bare `Assisted-by: Claude` without the model also counts.

Repos using [git-ai](https://github.com/git-ai-project/git-ai) get the highest-fidelity signal: its authorship logs under `refs/notes/ai` (Git AI Standard v3) record **which lines** each agent wrote. When notes are present on commits in the diff range, per-file AI line counts are *measured* from them instead of estimated by the diff heuristics, and the evidence names the agent and model (`AI-assisted commit a1b2c3d (git-ai: 6 AI line(s), cursor/claude-sonnet-4-5)`). AI lines are capped at each file's changed lines so authorship recorded outside the change can't inflate the ratio. Nothing changes on repos without git-ai. Note for CI: git notes aren't fetched by default — run `git fetch origin +refs/notes/ai:refs/notes/ai` after checkout.

This is attribution from signals the tools (or authors) volunteer, not forensic detection: stripping the trailer evades it, and the diff heuristics are only a low-confidence fallback.

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
| `--fail-on` | `critical` | Minimum severity that exits non-zero: `info`, `low`, `medium`, `high`, `critical` |
| `--json` | `false` | JSON output |
| `--format` | `summary` | Output format: `summary`, `detail`, `json` |

`ods analyze` also accepts file paths as positional arguments (`ods analyze a.go b.py`),
analyzing those files and skipping non-code ones — the entry point the pre-commit hook uses.

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
| `--ai-review` | — | AI review verdict file (`review-verdict/v1`); repeatable. Advisory: routes attention, never denies unless your policy opts in |

#### Disclosure completeness: `input.detection_sources`

The [SFC guidance](https://sfconservancy.org/) and the
[kernel docs](https://docs.kernel.org/process/coding-assistants.html) agree on
one duty: **the author discloses AI involvement** (what tool, what model, how
it participated) — it's not the maintainer's job to guess. ODS makes that norm
checkable at the gate. The policy input carries `detection_sources` — which
signals fired (`commit-trailer`, `git-ai-notes`, `pr-body`, `branch-name`,
`diff-heuristics`) — so a policy can separate *disclosed* AI use from merely
*suspected* AI use:

```rego
ai_disclosed { input.detection_sources[_] == "commit-trailer" }
ai_disclosed { input.detection_sources[_] == "git-ai-notes" }
ai_disclosed { input.detection_sources[_] == "pr-body" }

# Suspected AI without any author attribution → ask for it (never block)
warn[msg] {
    input.ai_generated == true
    not ai_disclosed
    msg = "AI code detected without author disclosure — ask for attribution"
}
```

The default policy ships this warning; the `ods init` scaffold additionally
routes undisclosed suspicion (confidence ≥ 0.5) to the `elevated` review tier.
Undisclosed AI costs review attention, never a merge — and adding a
`Co-Authored-By`/`Assisted-by` trailer or a PR-body disclosure silences the
nudge.

#### Evidence tier: `input.evidence_tier`

`detection_sources` says *which* signals fired; `evidence_tier` collapses them
into one ordered label for **how strong the attribution is** — a confidence
level, not forensic proof of authorship:

| Tier | Evidence | From |
|------|----------|------|
| `corroborated` | independently **measured** | `git-ai-notes` |
| `attested` | author/tool **declared** it | `commit-trailer`, `pr-body` |
| `inferred` | **heuristic** only | `branch-name`, `diff-heuristics` |

It's the strongest source present (`corroborated > attested > inferred`), empty
when nothing fired, derived deterministically from `detection_sources` — so it
adds no new detection, just a summary a policy can gate on directly:

```rego
# Grant auto-merge only when attribution is at least attested.
strong_evidence { input.evidence_tier == "corroborated" }
strong_evidence { input.evidence_tier == "attested" }

review_tier := "elevated" {
    input.ai_generated
    not strong_evidence          # inferred-only AI → extra eyes
}
```

An author who strips the trailer drops to a weaker tier — ODS surfaces what it
can see, it doesn't claim to unmask what hides. A low tier informs routing; it
never denies on its own.

#### Merge-confidence signals: `input.merge_confidence`

Static analysis tells you what's *wrong*; these tell you whether the change is
**shaped like real work** — deterministic facts derived from the diff alone, no
LLM, no stylometric guessing. The policy input carries `merge_confidence`:

| Field | Meaning |
|-------|---------|
| `added_source_without_tests` | Source code was added but no test file was added or updated (the most-cited "AI PR" smell) |
| `tests_touched` | Any test file was added or modified |
| `risky_paths` | Changed files on sensitive paths — CI config (`.github/workflows/`), dependency manifests/lockfiles, `auth`/`crypto`/`security` paths |
| `files_changed` / `net_added_lines` / `source_files_changed` / `test_files_changed` | Diff shape, for wide-but-shallow detection |

They are **facts, not opinions**: advisory by default (they route review
attention), and attribution is used to *raise the bar* — an AI-authored change
that adds source without tests, or touches a sensitive path, routes to
`elevated`. Deny stays opt-in:

```rego
# Default policy: warn + route AI changes; your policy can make it block.
warn[msg] {
    input.merge_confidence.added_source_without_tests
    msg = "Source code changed but no tests were added or updated"
}

# Opt in to enforce (deterministic, so it may deny):
deny[msg] {
    input.ai_generated
    input.merge_confidence.added_source_without_tests
    msg = "AI-authored change adds source without tests"
}
```

This is the achievable form of "is this PR safe to merge?": ODS proves the
change is tested, scanned, and shaped like real work — it does not judge whether
the code is *correct* (that needs a human or an AI reviewer). It reduces how
often a human has to reach that question.

#### Patch coverage: `input.patch_coverage`

`added_source_without_tests` answers "was *a* test touched?"; `patch_coverage`
answers the stronger, still-deterministic question: **is this change's new code
actually covered by tests?** ODS reads your existing coverage report (Go
`coverage.out`, LCOV `lcov.info`, or Cobertura `coverage.xml` — no new tooling)
and intersects the covered lines with the diff's *added* lines. The result is a
fraction in `input.patch_coverage`, or **`-1` when no coverage report is
present** — so policies must guard with `>= 0` before comparing:

```rego
# Default policy: AI-authored change whose added lines are under-covered → warn + elevate.
ai_low_patch_coverage {
    input.ai_generated == true
    input.patch_coverage >= 0        # guard: -1 means "not measured"
    input.patch_coverage < 0.8       # tune the threshold to your team
}

warn[msg] {
    ai_low_patch_coverage
    pct := round(input.patch_coverage * 100)
    msg = sprintf("AI-authored change: only %d%% of added lines are covered by tests", [pct])
}
```

Like the other merge-confidence signals it is advisory by default (warns and
routes AI changes to `elevated`), attribution only raises the bar, and deny
stays opt-in. NYC's `coverage-summary.json` is aggregate-only, so patch coverage
is skipped (not `-1`-penalized) for NYC-only repos; whole-project coverage still
flows through `input.test_coverage`.

#### Mutation score: `input.mutation_score`

Coverage proves a line *ran*; it can't prove a test would *catch a bug* in it —
the classic AI failure mode is green coverage over assertions that never fail.
Mutation testing closes that gap: it injects small faults ("mutants") and checks
whether the suite kills them. ODS is a **signal consumer**, not a test runner —
run [gremlins](https://github.com/go-gremlins/gremlins) in CI and point ODS at
its JSON report:

```bash
gremlins unleash --output gremlins.json ./...
ods check --mutation gremlins.json
```

ODS computes a **diff-scoped** mutation score — over only the mutants on the
change's *added* lines — as `killed / (killed + survived)` (a timed-out mutant
counts as killed; not-covered / not-viable mutants are excluded). The result is
`input.mutation_score`, or **`-1` when not measured** (no report, or no mutant on
a changed line), so guard with `>= 0`:

```rego
# AI-authored change whose new code has weak tests → warn + elevate.
warn[msg] {
    input.ai_generated
    input.mutation_score >= 0        # guard: -1 means "not measured"
    input.mutation_score < 0.5       # mutation scores run lower than coverage
    pct := round(input.mutation_score * 100)
    msg = sprintf("AI-authored change: tests kill only %d%% of mutations on the added lines", [pct])
}
```

Mutation testing is slower than the other signals (it re-runs the suite per
mutant), so it's opt-in: no `--mutation`, no `mutation_score`. Same posture as
the rest — advisory, attribution raises the bar, deny opt-in.

#### AI reviewer verdicts: `--ai-review`

Static analysis covers known defect patterns; AI code reviewers (Copilot code
review, CodeRabbit, Claude Code's `/review`, …) cover the semantic layer — is
the logic right, does the change do what it claims. `ods check --ai-review
<file>` (repeatable) feeds their conclusions into the same gate as a
[`review-verdict/v1`](https://github.com/open-delivery-spec/spec) JSON file:

```json
{
  "schema": "ods.dev/review-verdict/v1",
  "reviewer": {"tool": "claude-code", "model": "claude-sonnet-4-5"},
  "head_sha": "a1b2c3d",
  "verdict": "request_changes",
  "findings": [
    {"file": "src/auth.py", "line": 42, "severity": "high",
     "category": "correctness",
     "message": "Token expiry is never checked before refresh"}
  ]
}
```

The design principle: **deterministic findings may deny; probabilistic
opinions only route attention.** By default an AI review can only *tighten*
the gate — a `request_changes` verdict raises `review_tier` to `elevated`
(more human eyes) and adds a warning; it never denies, and an `approve` never
loosens anything (an LLM verdict steered by the code under review must not be
able to unlock auto-merge). Teams that want LLM findings to block can opt in
explicitly in their own Rego over `input.ai_reviews`:

```rego
deny[msg] {
    f := input.ai_reviews[_].findings[_]
    f.severity == "high"
    msg = sprintf("AI review high finding: %s", [f.message])
}
```

Verdicts stamped with a `head_sha` that doesn't match the current HEAD are
skipped with a warning — stale opinions about an older commit never enter the
gate. The LLM runs outside the gate; the gate stays deterministic.

In CI on `pull_request` events the checkout is a synthetic merge commit, so
`git rev-parse HEAD` is not the SHA reviewers stamped. Set `ODS_HEAD_SHA` to
the PR head SHA (`github.event.pull_request.head.sha`) and `check` compares
against that instead; the [validate-action](https://github.com/open-delivery-spec/validate-action)
does this automatically.

#### Review routing: `review_tier`

Beyond allow/deny, a policy can answer a second question: **how much human
attention does this change need?** Define a `review_tier` rule returning one of
`auto` (low risk — eligible for expedited review or auto-merge), `standard`
(normal review, the default), or `elevated` (high risk — request extra
reviewers):

```rego
default review_tier := "standard"

review_tier := "auto" {
    input.technical_debt_delta <= 1.0
    not has_high_or_critical
}

review_tier := "elevated" {
    input.ai_generated == true
    has_high_or_critical
}
```

The tier is reported in the text output and as `"review_tier"` in `--json`.
Semantics: **deny always wins** — a blocked PR is never routed; the tier is an
advisory routing signal for changes that may merge, and it never affects the
exit code. Policies that define no `review_tier` behave exactly as before
(consumers should treat the absent tier as `standard`). An unknown tier value
falls back to `standard` with a warning instead of failing the gate.
`ods init` scaffolds these rules (with explanatory comments) into new policies.

### `ods hook install` — Git Hooks

```bash
$ ods hook install
✅  pre-commit hook installed at .git/hooks/pre-commit
✅  prepare-commit-msg hook installed at .git/hooks/prepare-commit-msg
✅  pre-push hook installed at .git/hooks/pre-push
```

### pre-commit framework

Teams using [pre-commit](https://pre-commit.com) can add ODS's local quality
gate in one entry — the same analysis CI runs, but before you push:

```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/open-delivery-spec/cli
    rev: v0.7.4
    hooks:
      - id: ods-analyze          # blocks the commit on high/critical findings
```

Override the threshold per repo with `args: [--fail-on, critical]` (laxer) or
`[--fail-on, medium]` (stricter).

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

### `ods attest` — AI-Code Evidence Document

Serializes everything the gate just evaluated into an **auditable evidence
document**: a valid [CycloneDX 1.6](https://cyclonedx.org) BOM whose CDXA
declarations carry one claim per governance requirement (disclosure, evidence
grading, patch coverage, mutation score, policy verdict), each backed by
evidence with a **re-fetchable locator** (workflow-run URL, commit SHA) — the
artifact an "which code was AI-assisted, and what verification was applied?"
audit request actually asks for. Design:
[spec proposal 001](https://github.com/open-delivery-spec/spec/blob/main/docs/proposals/001-ai-code-evidence.md).

```bash
ods attest                          # writes evidence.cdx.json
ods attest --out -                  # print to stdout
ods attest --mutation gremlins.json # include the mutation-score requirement
```

Two invariants: **conformance carries the measured value, confidence carries
how the fact was obtained** (the evidence tier) — never merged; and the
document's own affirmation states that attribution is volunteered, not
forensic, and nothing in it asserts code correctness. Every emitted document
is schema-validated against the official CycloneDX 1.6 schema in CI.

| Flag | Default | Description |
|------|---------|-------------|
| `--out` | `evidence.cdx.json` | Output path (`-` for stdout) |
| `--policy`, `-p` | `.ods/policy.rego` | Policy whose verdict is attested |
| `--sarif` / `--ai-review` / `--mutation` / `--diff-base` | — | Same inputs as `ods check` — the attested facts are the gate's facts |

### `ods report` — AI Attribution Report

A governance view over recent history: how much delivered work is AI-assisted,
and trending which way. Attribution comes from the `Co-Authored-By` trailers AI
tools emit automatically and the kernel-style `Assisted-by:` trailers — the
same signals as `ods detect`. `Assisted-by` commits aggregate under their agent
name in the per-tool breakdown.

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

Add `--html ai-report.html` to get a **shareable dashboard** instead — hero
metrics, an "AI share over time" trend chart, and a per-tool breakdown, all in
one self-contained file (no external assets, opens offline, screenshots
cleanly):

```bash
ods report --since "1 year ago" --html ai-report.html
```

| Flag | Default | Description |
|------|---------|-------------|
| `--since` | `90 days ago` | History window (any git `--since` expression) |
| `--max-commits` | `0` | Cap commits scanned (0 = no cap) |
| `--json` | `false` | Machine-readable output (commit/line shares, per-tool counts) |
| `--html` | — | Write a self-contained HTML dashboard to this path (`-` for stdout) |

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
