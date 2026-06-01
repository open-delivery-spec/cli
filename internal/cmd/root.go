package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/open-delivery-spec/cli/internal/validator"
	"github.com/open-delivery-spec/cli/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile     string
	specVer     string
	strict      bool
	schemaDir   string
	policyProfile string
	policyFile  string
)

var rootCmd = &cobra.Command{
	Use:   "ods",
	Short: "Open Delivery Spec CLI",
	Long: `ods - Open Delivery Spec CLI

A command-line tool for validating, generating, and managing
delivery artifacts compliant with the Open Delivery Spec.

Validate branch names, commit messages, PR descriptions,
release readiness reports, rollback plans, and more.`,
	Version: version.Value,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if schemaDir != "" {
			return validator.LoadSchemasFromDir(schemaDir)
		}
		return validator.LoadEmbeddedSchemas()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default .ods.yaml)")
	rootCmd.PersistentFlags().StringVar(&specVer, "spec-version", "1.0.0", "ODS spec version")
	rootCmd.PersistentFlags().BoolVar(&strict, "strict", false, "treat warnings as errors")
	rootCmd.PersistentFlags().StringVar(&schemaDir, "schema-dir", "", "custom schema directory path")
	rootCmd.PersistentFlags().StringVar(&policyProfile, "profile", "", "compliance profile: oss, enterprise, regulated")
	rootCmd.PersistentFlags().StringVar(&policyFile, "policy", "", "path to policy YAML file")

	viper.BindPFlag("spec_version", rootCmd.PersistentFlags().Lookup("spec-version"))
	viper.BindPFlag("strict", rootCmd.PersistentFlags().Lookup("strict"))
	viper.BindPFlag("profile", rootCmd.PersistentFlags().Lookup("profile"))
	viper.BindPFlag("policy_file", rootCmd.PersistentFlags().Lookup("policy"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName(".ods")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.config/ods")
	}

	viper.SetEnvPrefix("ODS")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintf(os.Stderr, "Using config file: %s\n", viper.ConfigFileUsed())
	}
}
