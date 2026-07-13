package ods.policy

# Advisory profile — never blocks a merge.
# Surfaces every signal as a warning and still routes review attention, but
# the gate always passes. Ideal for adopting ODS incrementally or building
# team trust before turning on enforcement. Switch to `ods-way` or `strict`
# once the team is ready. Docs: https://github.com/open-delivery-spec/spec

default allow := true

# No deny rules — this profile is non-blocking by design.

warn[msg] {
    issue := input.issues[_]
    issue.severity == "critical"
    msg = sprintf("CRITICAL: %s at %s:%d — would block under ods-way/strict", [issue.rule, issue.file, issue.line])
}

warn[msg] {
    issue := input.issues[_]
    issue.severity == "high"
    msg = sprintf("HIGH: %s at %s:%d", [issue.rule, issue.file, issue.line])
}

warn[msg] {
    input.ai_generated == true
    count(input.issues) > 0
    msg = "AI-attributed change with quality findings — review recommended"
}

# Routing still works so teams get the signal without enforcement.
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
