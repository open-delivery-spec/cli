package ods.policy

# ODS way — the recommended default profile.
# Balanced and low-friction: block only critical issues, surface the rest,
# and route human attention by risk. This is the batteries-included starting
# point; edit freely or switch profiles with `ods init --profile <name>`.
# Docs: https://github.com/open-delivery-spec/spec

default allow := true

# Block AI or human code with critical quality issues.
deny[msg] {
    issue := input.issues[_]
    issue.severity == "critical"
    msg = sprintf("CRITICAL: %s at %s:%d", [issue.rule, issue.file, issue.line])
}

# Warn when high-confidence AI code carries multiple issues.
warn[msg] {
    input.ai_generated == true
    input.ai_confidence > 0.8
    count(input.issues) > 2
    msg = "High-confidence AI code with multiple quality issues — enhanced review recommended"
}

# Review routing: how much human attention this change needs.
# deny always wins — a blocked PR is never routed.
default review_tier := "standard"

review_tier := "auto" {
    input.technical_debt_delta <= 1.0
    not has_high_or_critical
}

review_tier := "elevated" {
    input.ai_generated == true
    has_high_or_critical
}

has_high_or_critical {
    input.issues[_].severity == "critical"
}

has_high_or_critical {
    input.issues[_].severity == "high"
}
