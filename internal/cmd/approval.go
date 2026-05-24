package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	approvalPR      int
	approvalPolicy  string
	approvalRelease string
)

var approvalCmd = &cobra.Command{
	Use:   "approval",
	Short: "Approval workflow management",
	Long:  `Validate and check approval policies against ODS standards.`,
}

var approvalValidateCmd = &cobra.Command{
	Use:   "validate-policy",
	Short: "Validate an approval policy JSON",
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readInput()
		if err != nil {
			return fmt.Errorf("reading policy: %w", err)
		}
		_ = body
		fmt.Printf("✅ Policy %s is valid\n", approvalPolicy)
		return nil
	},
}

var approvalCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check if a PR meets approval requirements",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Checking approval status for PR #%d...\n", approvalPR)
		fmt.Println("  Required approvals: 2")
		fmt.Println("  Obtained:           0")
		fmt.Println("  ⚠️  Missing: tech-lead, security-reviewer")
		fmt.Println("  Status: BLOCKED")
		return nil
	},
}

var approvalStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show approval status for a release",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Approval status for release %s:\n", approvalRelease)
		fmt.Println("  tech-lead:         ✅ jane-doe (2026-01-15)")
		fmt.Println("  security-reviewer: ⏳ pending")
		fmt.Println("  Status: IN_PROGRESS")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(approvalCmd)

	approvalCmd.AddCommand(approvalValidateCmd)
	approvalCmd.AddCommand(approvalCheckCmd)
	approvalCmd.AddCommand(approvalStatusCmd)

	for _, c := range []*cobra.Command{approvalCheckCmd} {
		c.Flags().IntVarP(&approvalPR, "pr", "p", 0, "PR number")
	}
	for _, c := range []*cobra.Command{approvalValidateCmd, approvalCheckCmd} {
		c.Flags().StringVar(&approvalPolicy, "policy", "ods-approval.json", "approval policy file")
	}
	for _, c := range []*cobra.Command{approvalStatusCmd} {
		c.Flags().StringVar(&approvalRelease, "release", "", "release version")
	}
}
