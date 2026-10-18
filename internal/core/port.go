package core

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

// CheckPortFree verifies that address can be bound before starting the core.
func CheckPortFree(address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return fmt.Errorf("address %s is already in use; choose another --mixed-port", address)
		}
		return fmt.Errorf("check address %s: %w", address, err)
	}
	if err := listener.Close(); err != nil {
		return fmt.Errorf("close port check for %s: %w", address, err)
	}
	return nil
}

// CheckPortInUse verifies that something is bound to address. It is used after
// startup to reject a silently failed mihomo bind.
func CheckPortInUse(address string) error {
	listener, err := net.Listen("tcp", address)
	if err == nil {
		_ = listener.Close()
		return fmt.Errorf("no listener is bound to %s", address)
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return nil
	}
	return fmt.Errorf("check listener %s: %w", address, err)
}
