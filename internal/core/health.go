package core

import (
	"context"
	"sync"
	"time"

	"github.com/metacubex/mihomo/adapter/outboundgroup"
	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
	mihomolog "github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/tunnel"
)

// initialHealthCheckTimeout bounds how long startup waits for url-test and
// fallback groups to obtain at least one measured node.
const initialHealthCheckTimeout = 10 * time.Second

func needsInitialHealthCheck(adapterType C.AdapterType) bool {
	switch adapterType {
	case C.URLTest, C.Fallback:
		return true
	default:
		return false
	}
}

// primeProxyGroups triggers the first health check for URL-test and fallback
// groups. Without this, their first dial can select the first member even when
// it is dead because no node has been measured yet.
func primeProxyGroups(ctx context.Context) {
	providers := make(map[string]P.ProxyProvider)
	for _, proxy := range tunnel.Proxies() {
		if !needsInitialHealthCheck(proxy.Adapter().Type()) {
			continue
		}
		group, ok := proxy.Adapter().(outboundgroup.ProxyGroup)
		if !ok {
			continue
		}
		for _, provider := range group.Providers() {
			providers[provider.Name()] = provider
		}
	}
	if len(providers) == 0 {
		return
	}

	started := time.Now()
	mihomolog.Infoln("Initial proxy health check started for %d provider(s)", len(providers))

	var wait sync.WaitGroup
	for _, provider := range providers {
		wait.Add(1)
		go func(provider P.ProxyProvider) {
			defer wait.Done()
			provider.HealthCheck()
		}(provider)
	}
	done := make(chan struct{})
	go func() {
		wait.Wait()
		close(done)
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(initialHealthCheckTimeout)
	defer timer.Stop()
	result := "completed"
	select {
	case <-done:
	case <-timer.C:
		result = "timed out"
	case <-ctx.Done():
		result = "canceled"
	}
	mihomolog.Infoln("Initial proxy health check %s after %s", result, time.Since(started).Round(time.Millisecond))
}
