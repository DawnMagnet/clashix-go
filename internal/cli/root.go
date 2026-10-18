// Package cli implements the cx command line interface.
package cli

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// NewRoot builds the cx command tree.
func NewRoot(version string) *cobra.Command {
	version = effectiveVersion(version)
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
	root.SetArgs(os.Args[1:])
	return root.Execute()
}

// effectiveVersion falls back to Go's build information when the release
// version was not injected with -ldflags.
func effectiveVersion(version string) string {
	if version != "" && version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	revision := ""
	dirty := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	if revision == "" {
		return version
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if dirty {
		revision += "-dirty"
	}
	return revision
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
