package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/DawnMagnet/clashix-go/internal/config"
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
			data, err := config.Marshal(cfg)
			if err != nil {
				return err
			}

			files := map[string][]byte{"config.yaml": data}
			if geodataDir != "" {
				entries, err := os.ReadDir(geodataDir)
				if err != nil {
					return fmt.Errorf("read geodata directory %q: %w", geodataDir, err)
				}
				for _, entry := range entries {
					if entry.IsDir() {
						continue
					}
					if entry.Name() == "config.yaml" {
						return fmt.Errorf("geodata directory must not contain config.yaml")
					}
					info, err := entry.Info()
					if err != nil {
						return fmt.Errorf("stat geodata file %q: %w", entry.Name(), err)
					}
					if !info.Mode().IsRegular() {
						continue
					}
					geodata, err := os.ReadFile(filepath.Join(geodataDir, entry.Name()))
					if err != nil {
						return fmt.Errorf("read geodata file %q: %w", entry.Name(), err)
					}
					files[entry.Name()] = geodata
				}
			}

			fmt.Fprintln(cmd.ErrOrStderr(), "WARNING: the packed binary contains a resolved configuration, including proxy credentials and the controller secret. Do not publish or distribute it.")
			if err := payload.Write(target, output, files); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", output)
			return err
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output binary path")
	cmd.Flags().StringVar(&geodataDir, "geodata", "", "directory containing geoip.metadb, geoip.dat, and geosite.dat")
	cmd.Flags().StringVar(&targetBinary, "target-binary", "", "binary to copy and pack (default: current executable)")
	addConfigFlags(cmd, &opts)
	return cmd
}
