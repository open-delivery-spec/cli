package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/open-delivery-spec/cli/internal/logx"
	"github.com/open-delivery-spec/cli/internal/policy"
	"github.com/spf13/cobra"
)

var (
	checkPolicyFile string
	checkJSON       bool
	checkSARIF      string
	checkDiffBase   string
	checkAIReviews  []string
	checkMutation   string
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Evaluate OPA Rego policy against a change",
	Long: `Evaluate an OPA Rego policy against the current change.

ODS uses OPA (Open Policy Agent) as its enterprise policy engine.
Write policies in Rego to define custom blocking rules.

Place a policy at .ods/policy.rego:

  package ods.policy

  default allow := true

  deny[msg] {
      input.ai_confidence > 0.8
      input.test_coverage < 0.3
      msg = "AI code with low test coverage"
  }

Examples:
  ods check                                    # use .ods/policy.rego or default
  ods check --policy .ods/policy.rego          # explicit policy file
  ods check --json                             # JSON output`,
	RunE: runCheck,
}

func init() {
	rootCmd.AddCommand(checkCmd)
	checkCmd.Flags().StringVarP(&checkPolicyFile, "policy", "p", "", "path to Rego policy file")
	checkCmd.Flags().BoolVar(&checkJSON, "json", false, "output as JSON")
	checkCmd.Flags().StringVar(&checkSARIF, "sarif", "",
		"SARIF v2.1.0 file whose findings are merged into the policy input")
	checkCmd.Flags().StringVar(&checkDiffBase, "diff-base", "",
		"git ref to diff against (default: $ODS_DIFF_BASE or HEAD~1)")
	checkCmd.Flags().StringArrayVar(&checkAIReviews, "ai-review", nil,
		"AI review verdict file (ods.dev/review-verdict/v1); repeatable. Advisory by default: routes review attention, never denies unless your policy opts in")
	checkCmd.Flags().StringVar(&checkMutation, "mutation", "",
		"mutation-testing report (gremlins JSON) whose diff-scoped mutation score is fed to the policy input")
}

func runCheck(cmd *cobra.Command, args []string) error {
	// Resolve diff base: ODS_DIFF_BASE is set by validate-action to the PR base SHA.
	// Prefer it over the hardcoded HEAD~1 so the full PR diff is analysed rather than
	// only the most-recent commit.
	diffBase := resolveDiffBase(checkDiffBase)
	// Keep the duplication estimator (which reads ODS_DIFF_BASE) on the same range.
	os.Setenv("ODS_DIFF_BASE", diffBase)
	logx.Debugf("check: diff base = %s", diffBase)

	// Discover policy file
	policyPath := checkPolicyFile
	if policyPath == "" {
		policyPath = policy.DiscoverRegoFile(".")
	}
	useDefault := false
	if policyPath == "" {
		useDefault = true
	}
	if useDefault {
		logx.Debugf("check: no policy file found, using built-in default policy")
	} else {
		logx.Debugf("check: using policy file %s", policyPath)
	}

	// Assemble the full policy input (detect → analyze → score → signals).
	evalInput, _, err := assembleGateInputs(cmd, diffBase, checkSARIF, checkAIReviews, checkMutation)
	if err != nil {
		return err
	}

	// Evaluate policy
	var result *policy.EvalResult
	if useDefault {
		// Use embedded default policy
		result, err = evaluateDefaultPolicy(evalInput)
	} else {
		result, err = policy.Evaluate(policyPath, evalInput)
	}
	if err != nil {
		return fmt.Errorf("policy evaluation failed: %w", err)
	}

	logx.Debugf("check: policy result allowed=%t denials=%d warnings=%d",
		result.Allowed, len(result.Denials), len(result.Warnings))
	for _, d := range result.Denials {
		logx.Debugf("check: DENY %s", d)
	}
	for _, w := range result.Warnings {
		logx.Debugf("check: WARN %s", w)
	}

	switch {
	case checkJSON:
		// Echo the deterministic merge-confidence facts the gate saw alongside
		// the result, so reports and auditors can render them without re-running
		// the pipeline. Embeds EvalResult so its fields stay top-level.
		out := struct {
			*policy.EvalResult
			MergeConfidence *policy.EvalMergeConfidence `json:"merge_confidence,omitempty"`
			PatchCoverage   float64                     `json:"patch_coverage"`
			MutationScore   float64                     `json:"mutation_score"`
			EvidenceTier    string                      `json:"evidence_tier,omitempty"`
		}{EvalResult: result, MergeConfidence: evalInput.MergeConfidence, PatchCoverage: evalInput.PatchCoverage, MutationScore: evalInput.MutationScore, EvidenceTier: evalInput.EvidenceTier}
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		cmd.OutOrStdout().Write(data)
		cmd.OutOrStdout().Write([]byte("\n"))
	default:
		printCheckResult(cmd, result, policyPath, useDefault)
	}

	if !result.Allowed {
		cmd.SilenceUsage = true
		return fmt.Errorf("policy denied: %d denial(s)", len(result.Denials))
	}
	return nil
}

func evaluateDefaultPolicy(input *policy.EvalInput) (*policy.EvalResult, error) {
	// Write default policy to temp file
	tmpFile, err := os.CreateTemp("", "ods-policy-*.rego")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(policy.DefaultRegoPolicy()); err != nil {
		return nil, err
	}
	tmpFile.Close()

	return policy.Evaluate(tmpFile.Name(), input)
}

func printCheckResult(cmd *cobra.Command, result *policy.EvalResult, policyPath string, useDefault bool) {
	if result.Allowed {
		fmt.Fprintf(cmd.OutOrStdout(), "✅  Policy check passed\n")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "❌  Policy check failed\n")
	}

	if useDefault {
		fmt.Fprintf(cmd.OutOrStdout(), "   Policy: default (built-in)\n")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "   Policy: %s\n", policyPath)
	}

	// Review tier only routes changes that may merge; a blocked PR needs a fix,
	// not a reviewer assignment.
	if result.ReviewTier != "" && result.Allowed {
		fmt.Fprintf(cmd.OutOrStdout(), "   Review tier: %s\n", result.ReviewTier)
	}

	if len(result.Denials) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "   Denials:\n")
		for _, d := range result.Denials {
			fmt.Fprintf(cmd.OutOrStdout(), "     ❌ %s\n", d)
		}
	}

	if len(result.Warnings) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "   Warnings:\n")
		for _, w := range result.Warnings {
			fmt.Fprintf(cmd.OutOrStdout(), "     ⚠️  %s\n", w)
		}
	}
}

// gitHeadSHA returns the commit that review verdicts are matched against.
// ODS_HEAD_SHA wins when set: CI checks out a synthetic merge commit on
// pull_request events, so `git rev-parse HEAD` is not the SHA reviewers
// stamped into their verdicts. Falls back to HEAD, or "" outside a git repo
// (callers treat the empty value as "nothing to compare against").
func gitHeadSHA() string {
	if sha := os.Getenv("ODS_HEAD_SHA"); sha != "" {
		return sha
	}
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
