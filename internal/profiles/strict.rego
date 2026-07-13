package ods.policy

# Strict profile — for regulated or high-stakes codebases.
# Blocks critical AND high-severity issues, and blocks AI-authored changes
# that ship below a test-coverage floor. Use when an escaped defect is
# expensive. Docs: https://github.com/open-delivery-spec/spec

default allow := true

# Block any critical or high-severity issue.
deny[msg] {
    issue := input.issues[_]
    issue.severity == "critical"
    msg = sprintf("CRITICAL: %s at %s:%d", [issue.rule, issue.file, issue.line])
}

deny[msg] {
    issue := input.issues[_]
    issue.severity == "high"
    msg = sprintf("HIGH: %s at %s:%d", [issue.rule, issue.file, issue.line])
}

# Block AI code that ships below 50% measured coverage.
# Guarded by test_coverage >= 0 so unmeasured coverage (-1) never false-blocks.
deny[msg] {
    input.ai_generated == true
    input.test_coverage >= 0
    input.test_coverage < 0.5
    pct := round(input.test_coverage * 100)
    msg = sprintf("AI code with %d%% test coverage (strict floor: 50%%)", [pct])
}

warn[msg] {
    input.ai_generated == true
    msg = "AI-attributed change — verify tests and reasoning"
}

# Review routing: auto only for clean, well-covered, low-debt changes.
default review_tier := "standard"

review_tier := "auto" {
    input.technical_debt_delta <= 1.0
    not has_high_or_critical
    input.test_coverage >= 0.6
}

review_tier := "elevated" {
    input.ai_generated == true
    has_high_or_critical
}

review_tier := "elevated" {
    input.ai_generated == true
    input.test_coverage >= 0
    input.test_coverage < 0.5
}

has_high_or_critical {
    input.issues[_].severity == "critical"
}

has_high_or_critical {
    input.issues[_].severity == "high"
}
