package ods.policy

# The ODS default policy.
#
# `ods check` applies it when the repository has no .ods/policy.rego, and
# `ods init` writes this same text as the starting point for a repository's
# own policy. Edit freely: these rules are a default, not a contract.
#
# Principle: deny only what is certain, warn and route the rest. Critical
# findings block. Probabilistic signals (AI attribution, reviewer verdicts,
# heuristic scores) never block on their own; they warn and raise the review
# tier so a human looks. Deny over any of them is opt-in: write your own rule.
#
# Field reference: https://open-delivery-spec.github.io/spec/schemas.html
# Patterns:        https://open-delivery-spec.github.io/spec/policy-authoring.html

default allow := true

# ── Deny: critical findings, unconditionally ──────────────────────────────
deny[msg] {
    issue := input.issues[_]
    issue.severity == "critical"
    msg = sprintf("CRITICAL: %s at %s:%d", [issue.rule, issue.file, issue.line])
}

# ── Warn: quality signals worth a look ────────────────────────────────────
warn[msg] {
    input.technical_debt_delta > 5.0
    msg = sprintf("Technical debt delta %v exceeds 5.0 — review the findings and the coverage gap before merging", [input.technical_debt_delta])
}

warn[msg] {
    input.ai_generated == true
    input.ai_confidence > 0.8
    count(input.issues) > 2
    msg = "High-confidence AI code with multiple quality issues — enhanced review recommended"
}

# test_coverage is -1 when no coverage report was found: guard with >= 0, or
# the rule fires on every repository without coverage tooling.
warn[msg] {
    input.ai_generated == true
    input.test_coverage >= 0
    input.test_coverage < 0.3
    pct := round(input.test_coverage * 100)
    msg = sprintf("AI-generated code has only %d%% test coverage", [pct])
}

# ── Disclosure completeness ───────────────────────────────────────────────
# SFC guidance and the kernel docs both require authors to disclose AI
# involvement (what tool, what model, how it participated). These rules make
# that norm checkable: AI signals without any author attribution get flagged
# and routed to extra review, never blocked.
ai_disclosed {
    input.detection_sources[_] == "commit-trailer"
}

ai_disclosed {
    input.detection_sources[_] == "git-ai-notes"
}

ai_disclosed {
    input.detection_sources[_] == "pr-body"
}

ai_undisclosed {
    input.ai_generated == true
    # Suspicion threshold for routing: tune to your tolerance for heuristic
    # false positives (branch prefix alone scores 0.35).
    input.ai_confidence >= 0.5
    not ai_disclosed
}

warn[msg] {
    input.ai_generated == true
    not ai_disclosed
    msg = "AI code detected without author disclosure — ask for attribution (Co-Authored-By/Assisted-by trailer, or an AI disclosure in the PR body)"
}

# ── Review routing ────────────────────────────────────────────────────────
# review_tier tells CI how much human attention this change needs:
#   "auto"     — low risk: eligible for expedited review or auto-merge
#   "standard" — normal review (the default)
#   "elevated" — high risk: request extra reviewers
# deny always wins: a blocked PR is never routed. Tune the conditions to your
# team; e.g. add "input.test_coverage >= 0.6" to auto if you publish coverage.
default review_tier := "standard"

review_tier := "auto" {
    input.technical_debt_delta <= 1.0
    not has_high_or_critical
    not ai_review_requests_changes
    not ai_undisclosed
    not ai_undertested
    not ai_touches_risky_path
    not ai_low_patch_coverage
    not ai_weak_mutation_score
}

review_tier := "elevated" {
    input.ai_generated == true
    has_high_or_critical
}

# Suspected-but-undisclosed AI costs review attention, never a merge.
review_tier := "elevated" {
    ai_undisclosed
}

# ── AI reviewer verdicts ──────────────────────────────────────────────────
# input.ai_reviews are probabilistic: by default they only tighten the gate.
# A request_changes routes extra human review, never denies. Write your own
# deny over input.ai_reviews to opt in to enforcement.
review_tier := "elevated" {
    ai_review_requests_changes
}

ai_review_requests_changes {
    input.ai_reviews[_].verdict == "request_changes"
}

warn[msg] {
    rev := input.ai_reviews[_]
    rev.verdict == "request_changes"
    msg = sprintf("AI reviewer %s requested changes (%d finding(s)) — extra review routed", [rev.tool, count(rev.findings)])
}

# ── Merge-confidence: deterministic diff facts ────────────────────────────
# Was source added without tests, does the change touch sensitive paths (CI
# config, dependency manifests, auth/crypto). They warn always, and route
# AI-authored changes to elevated. Deny stays opt-in: add your own deny over
# input.merge_confidence to enforce.
warn[msg] {
    input.merge_confidence.added_source_without_tests
    msg = "Source code changed but no tests were added or updated"
}

warn[msg] {
    p := input.merge_confidence.risky_paths[_]
    msg = sprintf("Change touches a sensitive path (%s) — extra review recommended", [p])
}

ai_undertested {
    input.ai_generated == true
    input.merge_confidence.added_source_without_tests
}

ai_touches_risky_path {
    input.ai_generated == true
    input.merge_confidence.risky_paths[_]
}

review_tier := "elevated" {
    ai_undertested
}

review_tier := "elevated" {
    ai_touches_risky_path
}

# ── Patch (diff) coverage: is this change's new code tested? ─────────────
# -1 means not measured.
ai_low_patch_coverage {
    input.ai_generated == true
    input.patch_coverage >= 0
    input.patch_coverage < 0.8
}

warn[msg] {
    ai_low_patch_coverage
    pct := round(input.patch_coverage * 100)
    msg = sprintf("AI-authored change: only %d%% of added lines are covered by tests", [pct])
}

review_tier := "elevated" {
    ai_low_patch_coverage
}

# ── Mutation score (diff-scoped): do the tests catch changes to the new ───
# code, not just run it? -1 means not measured. Scores run lower than
# coverage, so a lower threshold. Fed by `ods check --mutation <gremlins.json>`.
ai_weak_mutation_score {
    input.ai_generated == true
    input.mutation_score >= 0
    input.mutation_score < 0.5
}

warn[msg] {
    ai_weak_mutation_score
    pct := round(input.mutation_score * 100)
    msg = sprintf("AI-authored change: tests kill only %d%% of mutations on the added lines", [pct])
}

review_tier := "elevated" {
    ai_weak_mutation_score
}

has_high_or_critical {
    input.issues[_].severity == "critical"
}

has_high_or_critical {
    input.issues[_].severity == "high"
}
