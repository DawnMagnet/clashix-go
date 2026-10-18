package core

import (
	"net"
	"testing"
)

func TestPortChecks(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()

	if err := CheckPortFree(address); err == nil {
		t.Error("CheckPortFree succeeded while the port was occupied")
	}
	if err := CheckPortInUse(address); err != nil {
		t.Errorf("CheckPortInUse on an occupied port: %v", err)
	}

	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := CheckPortFree(address); err != nil {
		t.Errorf("CheckPortFree after close: %v", err)
	}
	if err := CheckPortInUse(address); err == nil {
		t.Error("CheckPortInUse succeeded on a free port")
	}
}
