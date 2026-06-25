package ods.policy

# ODS Enterprise Policy — Customize this file for your organization.
# Place at .ods/policy.rego in your repository root.

default allow := true

# ── Helper Rules ───────────────────────────────────────────────

# True when the diff contains at least one high or critical severity issue
has_serious_issues {
    input.issues[_].severity == "critical"
}

has_serious_issues {
    input.issues[_].severity == "high"
}

# ── Blocking Rules ─────────────────────────────────────────────

# Rule 1: Block critical quality issues unconditionally
deny[msg] {
    issue := input.issues[_]
    issue.severity == "critical"
    msg = sprintf("CRITICAL: %s at %s:%d — %s", [issue.rule, issue.file, issue.line, issue.message])
}

# Rule 2: Sensitive modules (auth/payment/billing) with AI code require ≥60% test coverage
deny[msg] {
    file := input.ai_files[_]
    regex.match(".*(auth|payment|billing|security|crypto).*", file.path)
    file.confidence > 0.5
    input.test_coverage >= 0
    input.test_coverage < 0.6
    msg = sprintf("AI code in sensitive module %s has %.0f%% test coverage (min 60%%)", [file.path, input.test_coverage * 100])
}

# Rule 3: Block high technical debt when serious quality issues are present.
# The debt delta is gated on high/critical issues because low/medium issues
# in small diffs can produce artificially high defect-density numbers; those
# are surfaced by Rule 5 (warn) rather than triggering a hard block.
deny[msg] {
    input.technical_debt_delta > 5.0
    has_serious_issues
    msg = sprintf("Technical debt increase %.1f exceeds block threshold (5.0) — high/critical severity issues present", [input.technical_debt_delta])
}

# ── Warning Rules ──────────────────────────────────────────────

# Rule 4: Warn when high-confidence AI code has low test coverage
warn[msg] {
    input.ai_generated == true
    input.ai_confidence > 0.7
    input.test_coverage >= 0
    input.test_coverage < 0.3
    msg = sprintf("High-confidence AI code (%.0f%%) with low test coverage (%.0f%%)", [input.ai_confidence * 100, input.test_coverage * 100])
}

# Rule 5: Warn on high defect density
warn[msg] {
    count(input.issues) > 5
    msg = sprintf("High defect count: %d issues found — enhanced review recommended", [count(input.issues)])
}

# Rule 6: Warn when AI code touches multiple sensitive modules
warn[msg] {
    paths := {path | file := input.ai_files[_]; path := file.path; regex.match(".*(auth|payment|billing|security).*", path)}
    count(paths) > 1
    msg = sprintf("AI code touches %d sensitive modules — consider splitting into separate PRs", [count(paths)])
}
