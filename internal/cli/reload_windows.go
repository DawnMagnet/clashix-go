//go:build windows

package cli

import "os"

func reloadSignals() []os.Signal {
	return nil
}
