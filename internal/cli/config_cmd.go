package cli

import (
	"fmt"

	"github.com/DawnMagnet/clashix-go/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	var (
		dir  string
		opts configOptions
	)

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect configuration",
	}
	show := &cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration with secrets redacted",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.validatePorts(cmd); err != nil {
				return err
			}
			activeDir, err := stateDir(dir)
			if err != nil {
				return err
			}
			names, embedded, err := payloadFiles()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), describePayload(names, embedded))

			cfg, err := loadEffectiveConfig(cmd.Context(), &opts, opts.overlay(cmd), true, config.DefaultConfigPath(activeDir))
			if err != nil {
				return err
			}
			data, err := config.MarshalIndent(config.Redact(cfg))
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), string(data))
			return err
		},
	}
	show.Flags().StringVar(&dir, "dir", "", "state directory (default: user config directory/cx)")
	addConfigFlags(show, &opts)
	cmd.AddCommand(show)
	return cmd
}
