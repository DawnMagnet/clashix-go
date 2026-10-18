//go:build windows

package cli

import "os"

func reloadSignals() []os.Signal {
	return nil
}

func stopSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
