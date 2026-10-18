package cli

import (
	"fmt"

	"github.com/DawnMagnet/clashix-go/internal/config"
	"github.com/spf13/cobra"
)

func newSubCmd() *cobra.Command {
	var (
		dir  string
		opts configOptions
	)

	cmd := &cobra.Command{
		Use:   "sub",
		Short: "Manage subscriptions",
	}
	update := &cobra.Command{
		Use:   "update",
		Short: "Fetch subscriptions and refresh the cached configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(opts.subscriptions) == 0 {
				return fmt.Errorf("at least one --sub URL is required")
			}
			if err := opts.validatePorts(cmd); err != nil {
				return err
			}
			stateDir, err := resolveStateDir(dir)
			if err != nil {
				return err
			}
			cfg, err := loadEffectiveConfig(cmd.Context(), &opts, opts.overlay(cmd), false, "")
			if err != nil {
				return err
			}
			path := config.DefaultConfigPath(stateDir)
			if err := config.Write(path, cfg); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), path)
			return err
		},
	}
	update.Flags().StringVar(&dir, "dir", "", "state directory (default: user config directory/cx)")
	addConfigFlags(update, &opts)
	cmd.AddCommand(update)
	return cmd
}
