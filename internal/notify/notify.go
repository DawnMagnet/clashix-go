// Package notify implements the small subset of sd_notify used by cx serve.
package notify

import "strings"

// Notifier sends lifecycle datagrams to systemd. The zero value and a notifier
// created without NOTIFY_SOCKET are no-ops.
type Notifier struct {
	conn notifyConn
}

// notifyConn is implemented by the platform notifier when notifications are
// enabled.
type notifyConn interface {
	write(message string) error
	close() error
}

func cleanStatus(status string) string {
	status = strings.ReplaceAll(status, "\r", " ")
	status = strings.ReplaceAll(status, "\n", " ")
	return strings.TrimSpace(status)
}

func (n *Notifier) send(message string) {
	if n == nil || n.conn == nil {
		return
	}
	_ = n.conn.write(message)
}

// Status sends a short STATUS= line.
func (n *Notifier) Status(status string) {
	if status = cleanStatus(status); status != "" {
		n.send("STATUS=" + status)
	}
}

// Ready sends READY=1 and an optional status line.
func (n *Notifier) Ready(status string) {
	message := "READY=1"
	if status = cleanStatus(status); status != "" {
		message += "\nSTATUS=" + status
	}
	n.send(message)
}

// Reloading sends RELOADING=1 with the CLOCK_MONOTONIC timestamp systemd
// requires, plus an optional status line.
func (n *Notifier) Reloading(status string) {
	if n == nil || n.conn == nil {
		return
	}
	message := "RELOADING=1\nMONOTONIC_USEC=" + monotonicUsec()
	if status = cleanStatus(status); status != "" {
		message += "\nSTATUS=" + status
	}
	n.send(message)
}

// Stopping sends STOPPING=1 and an optional status line.
func (n *Notifier) Stopping(status string) {
	message := "STOPPING=1"
	if status = cleanStatus(status); status != "" {
		message += "\nSTATUS=" + status
	}
	n.send(message)
}

// Close releases the notification socket.
func (n *Notifier) Close() error {
	if n == nil || n.conn == nil {
		return nil
	}
	return n.conn.close()
}
