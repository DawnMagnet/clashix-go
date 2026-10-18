package runner

import (
	"fmt"
	"net"
)

// FreePort returns an available TCP port on 127.0.0.1.
func FreePort() (int, error) {
	ports, err := FreePorts(1)
	if err != nil {
		return 0, err
	}
	return ports[0], nil
}

// FreePorts returns n distinct available TCP ports on 127.0.0.1. Listeners are
// held until all ports have been selected, so the returned ports do not
// collide with each other.
func FreePorts(n int) ([]int, error) {
	if n < 0 {
		return nil, fmt.Errorf("invalid port count %d", n)
	}
	if n == 0 {
		return nil, nil
	}

	listeners := make([]net.Listener, 0, n)
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()

	ports := make([]int, 0, n)
	for range n {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("pick free loopback port: %w", err)
		}
		listeners = append(listeners, listener)
		addr, ok := listener.Addr().(*net.TCPAddr)
		if !ok {
			return nil, fmt.Errorf("unexpected loopback address type %T", listener.Addr())
		}
		ports = append(ports, addr.Port)
	}
	return ports, nil
}
