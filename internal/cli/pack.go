package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DawnMagnet/clashix-go/internal/config"
	"github.com/DawnMagnet/clashix-go/internal/core"
	"github.com/DawnMagnet/clashix-go/internal/payload"
	"github.com/spf13/cobra"
)

func newPackCmd() *cobra.Command {
	var (
		output       string
		geodataDir   string
		targetBinary string
		opts         configOptions
	)

	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Pack a configuration and geodata into a cx binary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if output == "" {
				return fmt.Errorf("--output is required")
			}
			if err := opts.validatePorts(cmd); err != nil {
				return err
			}
			if err := opts.prepare(cmd); err != nil {
				return err
			}
			if dir, err := config.DefaultDir(); err == nil {
				opts.overrideDir = dir
			}
			target := targetBinary
			if target == "" {
				exe, err := os.Executable()
				if err != nil {
					return fmt.Errorf("locate current executable: %w", err)
				}
				target = exe
			}

			cfg, err := loadEffectiveConfig(cmd.Context(), &opts, opts.overlay(cmd), false, "")
			if err != nil {
				return err
			}
			if _, ok := cfg["secret"]; ok {
				delete(cfg, "secret")
				fmt.Fprintln(cmd.ErrOrStderr(), "cx pack: stripped the controller secret from the embedded config; serve will generate one at startup if the controller is enabled.")
			}
			data, err := config.Marshal(cfg)
			if err != nil {
				return err
			}

			files := map[string][]byte{"config.yaml": data}
			if geodataDir != "" {
				info, err := os.Stat(geodataDir)
				if err != nil {
					return fmt.Errorf("inspect geodata directory %q: %w", geodataDir, err)
				}
				if !info.IsDir() {
					return fmt.Errorf("geodata path %q is not a directory", geodataDir)
				}
				for _, name := range core.GeodataNames {
					path := filepath.Join(geodataDir, name)
					geodata, err := os.ReadFile(path)
					if errors.Is(err, os.ErrNotExist) {
						continue
					}
					if err != nil {
						return fmt.Errorf("read geodata file %q: %w", path, err)
					}
					files[name] = geodata
				}
			}

			if config.HasProxyCredentials(cfg, len(opts.subscriptions) > 0) {
				fmt.Fprintln(cmd.ErrOrStderr(), "")
				fmt.Fprintln(cmd.ErrOrStderr(), "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
				fmt.Fprintln(cmd.ErrOrStderr(), "WARNING: this binary embeds resolved proxy configuration and credentials.")
				fmt.Fprintln(cmd.ErrOrStderr(), "WARNING: keep it private. Never commit, upload, publish, or share the output.")
				fmt.Fprintln(cmd.ErrOrStderr(), "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
				fmt.Fprintln(cmd.ErrOrStderr(), "")
			}
			if err := payload.Write(target, output, files); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", output)
			return err
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output binary path")
	cmd.Flags().StringVar(&geodataDir, "geodata", "", "directory containing known geodata files")
	cmd.Flags().StringVar(&targetBinary, "target-binary", "", "binary to copy and pack (default: current executable)")
	addConfigFlags(cmd, &opts)
	return cmd
}
