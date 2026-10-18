// Package cli implements the cx command line interface.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewRoot builds the cx command tree.
func NewRoot(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "cx",
		Short:         "Clashix proxy runner and configuration tool",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("cx {{.Version}}\n")
	root.AddCommand(
		newServeCmd(),
		newRunCmd(),
		newSubCmd(),
		newPackCmd(),
		newConfigCmd(),
		newVersionCmd(version),
	)
	return root
}

// Execute runs the command tree.
func Execute(version string) error {
	return NewRoot(version).Execute()
}

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the cx version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "cx %s\n", version)
			return err
		},
	}
}
