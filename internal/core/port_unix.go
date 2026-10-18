//go:build !windows

package core

import (
	"errors"
	"syscall"
)

func isAddressInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}
