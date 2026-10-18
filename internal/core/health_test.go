package core

import (
	"testing"

	C "github.com/metacubex/mihomo/constant"
)

func TestNeedsInitialHealthCheck(t *testing.T) {
	for _, adapterType := range []C.AdapterType{C.URLTest, C.Fallback} {
		if !needsInitialHealthCheck(adapterType) {
			t.Errorf("needsInitialHealthCheck(%v) = false", adapterType)
		}
	}
	for _, adapterType := range []C.AdapterType{C.Selector, C.LoadBalance, C.Direct} {
		if needsInitialHealthCheck(adapterType) {
			t.Errorf("needsInitialHealthCheck(%v) = true", adapterType)
		}
	}
}
