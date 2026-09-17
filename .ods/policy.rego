# examples/ods-policy-oss-disclosure.rego
# Copy to .ods/policy.rego in your repository.
#
# Open-source disclosure policy: "AI assistance is welcome if you say so,
# test it, and own the result." It turns the AI clause most projects now put
# in CONTRIBUTING.md into a check that runs on every pull request:
#
#   1. Disclose it.  AI-assisted changes carry a Co-Authored-By / Assisted-by
#                    trailer, git-ai notes, or an AI-disclosure section in the
#                    PR body.
#   2. Test it.      AI-authored source comes with tests, and the added lines
#                    are covered when a coverage report exists.
#   3. Own it.       A human reviews every AI-assisted change; changes to CI
#                    config, dependencies, auth or crypto get extra eyes.
#
# Posture: nudge and route, never block on suspicion. The heuristics have
# false positives and a first-time contributor must never be turned away by
# a machine. Only deterministic findings deny (critical issues by default).
# Every STRICT block below is an opt-in deny for projects that want a hard
# gate once they have watched the warnings for a while.
#
# Attribution is volunteered, never proven: this policy makes honest
# disclosures count and gives the rule a place to live. It does not detect
# contributors who hide AI use, and it says nothing about correctness.
#
# Guide: https://open-delivery-spec.github.io/spec/oss-ai-policy.html

package ods.policy

default allow := true
default review_tier := "standard"

# ── 0. Hard gate: deterministic findings only ─────────────────────────────

deny[msg] {
    issue := input.issues[_]
    issue.severity == "critical"
    msg = sprintf("CRITICAL: %s at %s:%d", [issue.rule, issue.file, issue.line])
}

# ── 1. Disclose it ────────────────────────────────────────────────────────
# Disclosed: the author or their tool said so. Suspected: only ODS's own
# heuristics (branch name, diff patterns) say so.

ai_disclosed {
    input.detection_sources[_] == "commit-trailer"
}

ai_disclosed {
    input.detection_sources[_] == "git-ai-notes"
}

ai_disclosed {
    input.detection_sources[_] == "pr-body"
}

# Suspicion threshold for the nudge. An AI-tool branch prefix alone scores
# 0.6, the diff heuristics alone 0.4. Raise it if the nudge fires on human
# work too often; lower it if you want every hint to be checked.
ai_undisclosed {
    input.ai_generated
    input.ai_confidence >= 0.5
    not ai_disclosed
}

warn[msg] {
    ai_undisclosed
    msg = "AI assistance suspected but not disclosed — add a Co-Authored-By or Assisted-by trailer, or tick the AI disclosure in the PR description (see CONTRIBUTING.md)"
}

review_tier := "elevated" {
    ai_undisclosed
}

# STRICT (opt-in): refuse undisclosed AI changes outright. Keep the
# threshold high — a deny on a heuristic false positive turns away a human.
# deny[msg] {
#     ai_undisclosed
#     input.ai_confidence >= 0.8
#     msg = "This project requires AI assistance to be disclosed (see CONTRIBUTING.md)"
# }

# ── 2. Test it ────────────────────────────────────────────────────────────

ai_untested {
    input.ai_generated
    input.merge_confidence.added_source_without_tests
}

warn[msg] {
    ai_untested
    msg = "AI-assisted change adds source code but no test was added or updated (see CONTRIBUTING.md)"
}

review_tier := "elevated" {
    ai_untested
}

# Patch coverage of the added lines, only when a coverage report exists
# (-1 means not measured). 0.7 is a starting point; tune it to your suite.
ai_low_patch_coverage {
    input.ai_generated
    input.patch_coverage >= 0
    input.patch_coverage < 0.7
}

warn[msg] {
    ai_low_patch_coverage
    pct := round(input.patch_coverage * 100)
    msg = sprintf("AI-assisted change: only %d%% of its added lines are covered by tests (threshold 70%%)", [pct])
}

review_tier := "elevated" {
    ai_low_patch_coverage
}

# STRICT (opt-in): require tests for AI-authored source. Deterministic, so it
# may deny — but only for changes the author attested; a suspected change
# gets the nudge above instead.
# deny[msg] {
#     ai_untested
#     ai_disclosed
#     msg = "AI-assisted changes must come with tests (see CONTRIBUTING.md)"
# }

# ── 3. Own it ─────────────────────────────────────────────────────────────
# Human review cannot be measured by a machine; where the change lands can.
# risky_paths covers CI config, dependency manifests and lockfiles, and
# auth / crypto / security paths.

ai_touches_sensitive_path {
    input.ai_generated
    input.merge_confidence.risky_paths[_]
}

warn[msg] {
    ai_touches_sensitive_path
    p := input.merge_confidence.risky_paths[_]
    msg = sprintf("AI-assisted change touches a sensitive path (%s) — maintainer review required", [p])
}

review_tier := "elevated" {
    ai_touches_sensitive_path
}

# An AI reviewer's request_changes (fed in with --ai-review) only tightens
# the gate: more human attention, never a deny, and an approve never loosens.
ai_review_requests_changes {
    input.ai_reviews[_].verdict == "request_changes"
}

review_tier := "elevated" {
    ai_review_requests_changes
}

# ── Fast lane ─────────────────────────────────────────────────────────────
# Disclosed, tested, clean, low-debt AI changes are the easy ones. "auto"
# marks them eligible for expedited review. It never merges anything by
# itself; see the validate-action review-routing docs for what it does.

has_high_or_critical {
    input.issues[_].severity == "critical"
}

has_high_or_critical {
    input.issues[_].severity == "high"
}

review_tier := "auto" {
    input.ai_generated
    ai_disclosed
    input.merge_confidence.tests_touched
    input.technical_debt_delta <= 1.0
    not has_high_or_critical
    not ai_undisclosed
    not ai_untested
    not ai_low_patch_coverage
    not ai_touches_sensitive_path
    not ai_review_requests_changes
}
