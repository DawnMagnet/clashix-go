// Package cli implements the cx command line interface.
package cli

import (
	"fmt"
	"os"
	"strings"

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
	root := NewRoot(version)
	root.SetArgs(normalizeUIArgs(os.Args[1:]))
	return root.Execute()
}

// normalizeUIArgs allows --ui to be used both bare (default bind address) and
// with a space-separated address. pflag's NoOptDefVal normally requires
// --ui=ADDR for an explicit value.
func normalizeUIArgs(args []string) []string {
	normalized := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			normalized = append(normalized, args[i:]...)
			break
		}
		if arg == "--ui" && i+1 < len(args) {
			next := args[i+1]
			if next != "--" && !strings.HasPrefix(next, "-") && strings.Contains(next, ":") {
				normalized = append(normalized, "--ui="+next)
				i++
				continue
			}
		}
		normalized = append(normalized, arg)
	}
	return normalized
}

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the cx version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "cx %s\n", version); err != nil {
				return err
			}
			names, embedded, err := payloadFiles()
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), describePayload(names, embedded))
			return err
		},
	}
}
