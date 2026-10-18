//go:build windows

package core

import (
	"errors"
	"strings"
	"syscall"
)

const wsaEADDRINUSE = syscall.Errno(10048)

func isAddressInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, wsaEADDRINUSE) {
		return true
	}
	// Windows net errors have historically used this message instead of
	// exposing a stable sentinel through every layer.
	return strings.Contains(strings.ToLower(err.Error()), "only one usage of each socket address")
}
