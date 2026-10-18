//go:build !linux

package notify

// New returns a no-op notifier on platforms without sd_notify support.
func New() *Notifier {
	return &Notifier{}
}

func monotonicUsec() string {
	return "0"
}
