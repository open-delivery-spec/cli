package cmd

import (
	"fmt"
	"os"

	"github.com/open-delivery-spec/cli/internal/validator"
	"github.com/spf13/cobra"
)

var (
	validateFile  string
	validateStdin bool
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate artifacts against ODS schemas",
	Long:  `Validate delivery artifacts (branches, commits, PRs, etc.) against Open Delivery Spec JSON Schemas.`,
}

var validateBranchCmd = &cobra.Command{
	Use:   "branch [name]",
	Short: "Validate a branch name",
	Long: `Validate a branch name against the ODS Branch Naming spec.

Examples:
  ods validate branch feature/add-oauth-login
  ods validate branch bugfix/fix-null-pointer --strict`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		result, err := validator.ValidateBranch(name)
		if err != nil {
			return err
		}
		return printResult(result)
	},
}

var validateCommitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Validate a commit message",
	Long: `Validate a commit message against the ODS Commit Message spec.

Examples:
  ods validate commit --file commit-msg.txt
  git log -1 --format=%B | ods validate commit --stdin`,
	RunE: func(cmd *cobra.Command, args []string) error {
		msg, err := readInput()
		if err != nil {
			return fmt.Errorf("reading commit message: %w", err)
		}
		result, err := validator.ValidateCommitMessage(msg)
		if err != nil {
			return err
		}
		return printResult(result)
	},
}

var validatePRCmd = &cobra.Command{
	Use:   "pr",
	Short: "Validate a PR description",
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readInput()
		if err != nil {
			return fmt.Errorf("reading PR description: %w", err)
		}
		result, err := validator.ValidatePRDescription(body)
		if err != nil {
			return err
		}
		return printResult(result)
	},
}

var validateRollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Validate a rollback plan",
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readInput()
		if err != nil {
			return fmt.Errorf("reading rollback plan: %w", err)
		}
		result, err := validator.ValidateRollbackPlan(body)
		if err != nil {
			return err
		}
		return printResult(result)
	},
}

var validateEvidenceCmd = &cobra.Command{
	Use:   "evidence",
	Short: "Validate an evidence bundle",
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readInput()
		if err != nil {
			return fmt.Errorf("reading evidence bundle: %w", err)
		}
		result, err := validator.ValidateEvidence(body)
		if err != nil {
			return err
		}
		return printResult(result)
	},
}

var validateReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Validate a release readiness report",
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readInput()
		if err != nil {
			return fmt.Errorf("reading release report: %w", err)
		}
		result, err := validator.ValidateReleaseReadiness(body)
		if err != nil {
			return err
		}
		return printResult(result)
	},
}

var validateApprovalCmd = &cobra.Command{
	Use:   "approval-policy",
	Short: "Validate an approval policy",
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
	rootCmd.AddCommand(validateCmd)

	validateCmd.AddCommand(validateBranchCmd)
	validateCmd.AddCommand(validateCommitCmd)
	validateCmd.AddCommand(validatePRCmd)
	validateCmd.AddCommand(validateRollbackCmd)
	validateCmd.AddCommand(validateEvidenceCmd)
	validateCmd.AddCommand(validateReleaseCmd)
	validateCmd.AddCommand(validateApprovalCmd)

	// shared flags for file/stdin reading
	for _, c := range []*cobra.Command{validateCommitCmd, validatePRCmd, validateRollbackCmd, validateEvidenceCmd, validateReleaseCmd, validateApprovalCmd} {
		c.Flags().StringVarP(&validateFile, "file", "f", "", "input file path")
		c.Flags().BoolVar(&validateStdin, "stdin", false, "read from stdin")
	}
}

func readInput() (string, error) {
	if validateFile != "" {
		data, err := os.ReadFile(validateFile)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	// read from stdin if piped or --stdin flag
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 || validateStdin {
		data, err := ioReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return "", fmt.Errorf("no input provided: use --file or pipe input via stdin")
}

func printResult(r validator.Result) error {
	switch r.Status {
	case validator.StatusConformant:
		fmt.Println("✅ conformant — all requirements satisfied")
	case validator.StatusConformantWarnings:
		fmt.Println("⚠️  conformant with warnings")
		for _, w := range r.Warnings {
			fmt.Printf("   - %s\n", w)
		}
	case validator.StatusNonConformant:
		fmt.Println("❌ non-conformant")
		for _, e := range r.Errors {
			fmt.Printf("   - %s\n", e)
		}
		if strict && len(r.Warnings) > 0 {
			fmt.Println("\nWarnings (treated as errors in strict mode):")
			for _, w := range r.Warnings {
				fmt.Printf("   - %s\n", w)
			}
			return fmt.Errorf("validation failed with %d error(s) and %d warning(s)", len(r.Errors), len(r.Warnings))
		}
		if r.Status == validator.StatusNonConformant {
			return fmt.Errorf("validation failed with %d error(s)", len(r.Errors))
		}
	}
	return nil
}

// ioReadAll avoids importing io/ioutil (deprecated) and io for minimal deps
func ioReadAll(f *os.File) ([]byte, error) {
	var buf []byte
	b := make([]byte, 4096)
	for {
		n, err := f.Read(b)
		buf = append(buf, b[:n]...)
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return buf, err
		}
		if n == 0 {
			break
		}
	}
	return buf, nil
}
