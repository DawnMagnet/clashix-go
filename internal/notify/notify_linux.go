//go:build linux

package notify

import (
	"net"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type unixgramConn struct {
	conn *net.UnixConn
}

func (c *unixgramConn) write(message string) error {
	_, err := c.conn.Write([]byte(message))
	return err
}

func (c *unixgramConn) close() error {
	return c.conn.Close()
}

// New connects to NOTIFY_SOCKET. It returns a no-op notifier when the variable
// is unset or the socket cannot be reached.
func New() *Notifier {
	address := strings.TrimSpace(os.Getenv("NOTIFY_SOCKET"))
	if address == "" || (address[0] != '/' && address[0] != '@') {
		return &Notifier{}
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
	if err != nil {
		return &Notifier{}
	}
	return &Notifier{conn: &unixgramConn{conn: conn}}
}

func monotonicUsec() string {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return "0"
	}
	return strconv.FormatInt(ts.Sec*1_000_000+ts.Nsec/1_000, 10)
}
