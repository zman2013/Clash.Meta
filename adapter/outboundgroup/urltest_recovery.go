package outboundgroup

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/provider"
	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
)

const (
	androidRecentProbe      = 2 * time.Minute
	androidRecoveryCooldown = 15 * time.Second
)

type androidURLTest struct {
	sync.Mutex
	cachedAt, lastUsed         time.Time
	revision                   uint64
	freshAfter, lastRecovery   time.Time
	recovering, networkPending bool
	failedNode                 C.Proxy
	failedAt                   time.Time
}

func latestProbe(proxy C.Proxy, url string) C.DelayHistory {
	if p, ok := proxy.(interface{ DelayHistoryForTestUrl(string) []C.DelayHistory }); ok {
		history := p.DelayHistoryForTestUrl(url)
		if len(history) > 0 {
			return history[len(history)-1]
		}
	}
	return C.DelayHistory{}
}

func (u *URLTest) fastAndroid(touch bool) C.Proxy {
	state := u.android
	now := time.Now()
	state.Lock()
	defer state.Unlock()
	if touch {
		state.lastUsed = now
		u.Touch()
	}
	revision := adapter.URLTestRevision.Load()
	if revision != state.revision || now.Sub(state.cachedAt) >= time.Second {
		u.fastNode = u.pickAndroid(u.GetProxies(false))
		state.revision, state.cachedAt = revision, now
	}
	return u.fastNode
}

func (u *URLTest) pickAndroid(proxies []C.Proxy) C.Proxy {
	state := u.android
	var best, fresh, incumbent C.Proxy
	bestDelay, freshDelay := uint16(math.MaxUint16), uint16(math.MaxUint16)
	for _, proxy := range proxies {
		if !proxy.AliveForTestUrl(u.testUrl) {
			continue
		}
		if proxy.Name() == u.selected {
			return proxy
		}
		delay := proxy.LastDelayForTestUrl(u.testUrl)
		if delay == math.MaxUint16 {
			continue
		}
		if proxy == state.failedNode && latestProbe(proxy, u.testUrl).Time.Before(state.failedAt) {
			continue
		}
		if proxy == u.fastNode {
			incumbent = proxy
		}
		if delay < bestDelay {
			best, bestDelay = proxy, delay
		}
		if !state.freshAfter.IsZero() && delay <= provider.AndroidGoodDelay && delay < freshDelay &&
			!latestProbe(proxy, u.testUrl).Time.Before(state.freshAfter) {
			fresh, freshDelay = proxy, delay
		}
	}
	if fresh != nil {
		best, bestDelay = fresh, freshDelay
		if incumbent != nil && latestProbe(incumbent, u.testUrl).Time.Before(state.freshAfter) {
			incumbent = nil
		}
	}
	if incumbent != nil && int(incumbent.LastDelayForTestUrl(u.testUrl)) <= int(bestDelay)+int(u.tolerance) {
		return incumbent
	}
	if best != nil {
		return best
	}
	return proxies[0]
}

// RefreshAndroid responds to an Android service wake or a change in the
// underlying network. Groups that have not routed traffic need no recovery.
func (u *URLTest) RefreshAndroid(networkChanged bool) {
	state := u.android
	if state == nil {
		return
	}
	// The monotonic clock can stop during Android deep sleep.
	now := time.Now().Round(0)
	state.Lock()
	defer state.Unlock()
	if state.lastUsed.IsZero() || u.selected != "" {
		return
	}
	if !networkChanged {
		current := latestProbe(u.fastNode, u.testUrl)
		if current.Delay > 0 && current.Delay <= provider.AndroidGoodDelay && now.Sub(current.Time) < androidRecentProbe {
			return
		}
	}
	u.startAndroidRecovery(now, networkChanged)
}

func (u *URLTest) androidDialFailed(proxy C.Proxy, err error) {
	state := u.android
	if state == nil || errors.Is(err, context.Canceled) || errors.Is(err, C.ErrNotSupport) {
		return
	}
	switch proxy.Type() {
	case C.Direct, C.Compatible, C.Reject, C.Pass, C.RejectDrop:
		return
	}
	state.Lock()
	defer state.Unlock()
	if proxy != u.fastNode || u.selected != "" {
		return
	}
	if !state.recovering && time.Since(state.lastRecovery) >= androidRecoveryCooldown {
		state.failedNode = proxy
		state.failedAt = time.Now().Round(0)
		u.startAndroidRecovery(state.failedAt, false)
	}
}

// The caller holds the Android selection lock.
func (u *URLTest) startAndroidRecovery(now time.Time, networkChanged bool) {
	state := u.android
	if state.recovering {
		state.networkPending = state.networkPending || networkChanged
		return
	}
	if !networkChanged && now.Sub(state.lastRecovery) < androidRecoveryCooldown {
		return
	}
	state.recovering = true
	state.freshAfter = now
	state.lastRecovery = now
	state.cachedAt = time.Time{}
	expectedStatus, _ := utils.NewUnsignedRanges[uint16](u.expectedStatus)
	go func() {
		provider.RecoverAndroidProxies(u.GetProxies(false), u.testUrl, time.Duration(u.TestTimeout)*time.Millisecond, expectedStatus)
		state.Lock()
		defer state.Unlock()
		state.recovering = false
		pending := state.networkPending
		state.networkPending = false
		if pending && u.selected == "" {
			u.startAndroidRecovery(time.Now().Round(0), true)
		}
	}()
}
