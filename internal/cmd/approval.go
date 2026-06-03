package cmd

import (
	"fmt"

	"github.com/open-delivery-spec/cli/internal/validator"
	"github.com/spf13/cobra"
)

var (
	approvalPR    int
	approvalFile  string
)

var approvalCmd = &cobra.Command{
	Use:   "approval",
	Short: "Approval workflow management",
	Long:  `Validate and check approval policies against ODS standards.`,
}

var approvalValidatePolicyCmd = &cobra.Command{
	Use:   "validate-policy",
	Short: "Validate an approval policy JSON file against ODS schema",
	Long: `Validate an ODS approval policy JSON file against the ODS Approval Workflow schema.

Checks:
  - Required fields: policy_id, version, rules, roles
  - At least one rule defined
  - Valid rule structure

Examples:
  ods approval validate-policy --file ods-approval.json
  cat ods-approval.json | ods approval validate-policy --stdin`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readInput()
		if err != nil {
			return fmt.Errorf("reading approval policy: %w", err)
		}
		result, err := validator.ValidateApprovalPolicy(body)
		if err != nil {
			return err
		}
		return printResult(result)
	},
}

func init() {
	rootCmd.AddCommand(approvalCmd)

	approvalCmd.AddCommand(approvalValidatePolicyCmd)

	approvalValidatePolicyCmd.Flags().StringVarP(&validateFile, "file", "f", "", "approval policy JSON file")
	approvalValidatePolicyCmd.Flags().BoolVar(&validateStdin, "stdin", false, "read from stdin")
}
