package main

import (
	"github.com/spf13/cobra"
)

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "assistant",
		Short:         "Personal assistant service",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	var configPath string
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the HTTP assistant service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), configPath)
		},
	}
	runCmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "path to YAML config")

	root.AddCommand(runCmd)
	return root
}
