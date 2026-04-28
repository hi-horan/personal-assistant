package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

const defaultConfigPath = "config.yaml"

func newRootCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:           "assistant",
		Short:         "Personal AI assistant service",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVarP(&configPath, "config", "c", defaultConfigPath, "path to YAML config file")
	cmd.AddCommand(newRunCommand(&configPath))
	cmd.AddCommand(newMigrateCommand(&configPath))
	cmd.AddCommand(newDoctorCommand(&configPath))
	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "personal-assistant phase0")
			return err
		},
	})

	return cmd
}
