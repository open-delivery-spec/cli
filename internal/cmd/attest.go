package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/open-delivery-spec/cli/internal/evidence"
	"github.com/open-delivery-spec/cli/internal/logx"
	"github.com/open-delivery-spec/cli/internal/policy"
	"github.com/open-delivery-spec/cli/internal/version"
	"github.com/spf13/cobra"
)

var (
	attestPolicyFile string
	attestSARIF      string
	attestDiffBase   string
	attestAIReviews  []string
	attestMutation   string
	attestOut        string
)

var attestCmd = &cobra.Command{
	Use:   "attest",
	Short: "Emit an AI-code evidence document (CycloneDX 1.6)",
	Long: `Serialize the facts the ODS pipeline computed for this change into an
auditable AI-code evidence document — a valid CycloneDX 1.6 BOM whose CDXA
declarations carry per-requirement claims backed by re-fetchable evidence
(spec: docs/proposals/001-ai-code-evidence.md).

The document is assembled from the exact same pipeline ods check evaluates:
attribution and evidence tier, diff-scoped patch coverage and mutation score,
merge-confidence facts, and the policy verdict. It records evidence with graded
confidence; it does not prove authorship or code correctness.

Examples:
  ods attest                          # writes evidence.cdx.json
  ods attest --out -                  # print to stdout
  ods attest --mutation gremlins.json # include the mutation-score requirement`,
	RunE: runAttest,
}

func init() {
	rootCmd.AddCommand(attestCmd)
	attestCmd.Flags().StringVarP(&attestPolicyFile, "policy", "p", "", "path to Rego policy file")
	attestCmd.Flags().StringVar(&attestSARIF, "sarif", "",
		"SARIF v2.1.0 file whose findings are merged into the policy input")
	attestCmd.Flags().StringVar(&attestDiffBase, "diff-base", "",
		"git ref to diff against (default: $ODS_DIFF_BASE or HEAD~1)")
	attestCmd.Flags().StringArrayVar(&attestAIReviews, "ai-review", nil,
		"AI review verdict file (ods.dev/review-verdict/v1); repeatable")
	attestCmd.Flags().StringVar(&attestMutation, "mutation", "",
		"mutation-testing report (gremlins JSON) whose diff-scoped score is attested")
	attestCmd.Flags().StringVar(&attestOut, "out", "evidence.cdx.json",
		`output path for the evidence document ("-" for stdout)`)
}

func runAttest(cmd *cobra.Command, args []string) error {
	diffBase := resolveDiffBase(attestDiffBase)
	os.Setenv("ODS_DIFF_BASE", diffBase)
	logx.Debugf("attest: diff base = %s", diffBase)

	// Same policy discovery as check: the attested verdict must be the gate's.
	policyPath := attestPolicyFile
	if policyPath == "" {
		policyPath = policy.DiscoverRegoFile(".")
	}

	evalInput, detectResult, err := assembleGateInputs(cmd, diffBase, attestSARIF, attestAIReviews, attestMutation)
	if err != nil {
		return err
	}

	var result *policy.EvalResult
	if policyPath == "" {
		result, err = evaluateDefaultPolicy(evalInput)
	} else {
		result, err = policy.Evaluate(policyPath, evalInput)
	}
	if err != nil {
		return fmt.Errorf("policy evaluation failed: %w", err)
	}

	meta := evidence.Meta{
		Repo:              os.Getenv("GITHUB_REPOSITORY"),
		PR:                prNumberFromRef(os.Getenv("GITHUB_REF")),
		HeadSHA:           headSHAForAttest(),
		DiffBase:          diffBase,
		Branch:            evalInput.Branch,
		RunURL:            runURLFromEnv(),
		ToolVersion:       version.Value,
		PipelineIntegrity: os.Getenv("ODS_PIPELINE_INTEGRITY"),
	}

	doc := evidence.Build(evalInput, detectResult, result, meta)
	data, err := doc.Marshal()
	if err != nil {
		return fmt.Errorf("marshaling evidence document: %w", err)
	}
	data = append(data, '\n')

	if attestOut == "-" {
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}
	if err := os.WriteFile(attestOut, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", attestOut, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "AI-code evidence document written to %s\n", attestOut)
	return nil
}

// runURLFromEnv builds the workflow-run URL — the document's primary
// re-fetchable locator — from the standard GitHub Actions environment.
func runURLFromEnv() string {
	server := os.Getenv("GITHUB_SERVER_URL")
	repo := os.Getenv("GITHUB_REPOSITORY")
	run := os.Getenv("GITHUB_RUN_ID")
	if server == "" || repo == "" || run == "" {
		return ""
	}
	return server + "/" + repo + "/actions/runs/" + run
}

// prNumberFromRef extracts the PR number from a refs/pull/<n>/merge ref.
func prNumberFromRef(ref string) string {
	parts := strings.Split(ref, "/")
	if len(parts) >= 3 && parts[0] == "refs" && parts[1] == "pull" {
		return parts[2]
	}
	return ""
}

// headSHAForAttest prefers the CI-provided head SHA (PR checkouts are
// synthetic merge commits) and falls back to the local HEAD.
func headSHAForAttest() string {
	if sha := os.Getenv("ODS_HEAD_SHA"); sha != "" {
		return sha
	}
	return gitHeadSHA()
}
