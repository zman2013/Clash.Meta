package provider

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
)

const (
	AndroidGoodDelay           = 1500
	androidRecoveryShortlist   = 4
	androidRecoveryConcurrency = 10
	androidRecoveryExpandAfter = 900 * time.Millisecond
	androidRecoverySearchTime  = 30 * time.Second
)

func androidProbeCandidates(proxies []C.Proxy, url string, now time.Time) []C.Proxy {
	type candidate struct {
		proxy       C.Proxy
		good, total int
		median      uint16
	}
	candidates := make([]candidate, 0, len(proxies))
	for _, proxy := range proxies {
		if !isAndroidProbeCandidate(proxy) {
			continue
		}
		item := candidate{proxy: proxy}
		if p, ok := proxy.(interface{ DelayHistoryForTestUrl(string) []C.DelayHistory }); ok {
			var delays []uint16
			for _, entry := range p.DelayHistoryForTestUrl(url) {
				if now.Sub(entry.Time) > 2*time.Hour {
					continue
				}
				item.total++
				if entry.Delay > 0 && entry.Delay <= AndroidGoodDelay {
					delays = append(delays, entry.Delay)
				}
			}
			item.good = len(delays)
			if item.good > 0 {
				sort.Slice(delays, func(i, j int) bool { return delays[i] < delays[j] })
				item.median = delays[item.good/2]
			}
		}
		candidates = append(candidates, item)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		// A prior of one success and one failure keeps unknown nodes ahead
		// of repeatedly failing nodes and accounts for the sample size.
		aScore, bScore := (a.good+1)*(b.total+2), (b.good+1)*(a.total+2)
		if aScore != bScore {
			return aScore > bScore
		}
		if a.good != b.good {
			return a.good > b.good
		}
		return a.median < b.median
	})
	sorted := make([]C.Proxy, 0, len(candidates))
	for _, c := range candidates {
		sorted = append(sorted, c.proxy)
	}
	return sorted
}

type androidProbeKey struct {
	proxy       C.Proxy
	url, status string
}

type androidProbeCall struct {
	done  chan struct{}
	delay uint16
}

var androidProbes sync.Map

func probeAndroid(ctx context.Context, proxy C.Proxy, url string, expectedStatus utils.IntRanges[uint16]) uint16 {
	key := androidProbeKey{proxy: proxy, url: url, status: expectedStatus.String()}
	call := &androidProbeCall{done: make(chan struct{})}
	if existing, loaded := androidProbes.LoadOrStore(key, call); loaded {
		active := existing.(*androidProbeCall)
		select {
		case <-active.done:
			return active.delay
		case <-ctx.Done():
			return 0
		}
	}
	defer androidProbes.Delete(key)
	defer close(call.done)
	if ctx.Err() == nil {
		delay, err := proxy.URLTest(ctx, url, expectedStatus)
		if err == nil && proxy.AliveForTestUrl(url) {
			call.delay = delay
		}
	}
	return call.delay
}

// RecoverAndroidProxies searches in the background. A good result is visible
// through the proxy immediately. Already issued probes finish normally so
// closing the shortlist cannot record a healthy node as failed.
func RecoverAndroidProxies(proxies []C.Proxy, url string, timeout time.Duration, expectedStatus utils.IntRanges[uint16]) {
	candidates := androidProbeCandidates(proxies, url, time.Now())
	expand := time.NewTimer(androidRecoveryExpandAfter)
	defer expand.Stop()
	search := time.NewTimer(androidRecoverySearchTime)
	defer search.Stop()
	results := make(chan uint16, androidRecoveryConcurrency)
	recoverAndroidProxies(candidates, expand.C, search.C, results, func(proxy C.Proxy) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			results <- probeAndroid(ctx, proxy, url, expectedStatus)
		}()
	})
}

func recoverAndroidProxies(candidates []C.Proxy, expand, search <-chan time.Time, results <-chan uint16, start func(C.Proxy)) {
	next, pending := 0, 0
	limit := androidRecoveryShortlist
	stopped := false
	for {
		for !stopped && pending < limit && next < len(candidates) {
			start(candidates[next])
			next++
			pending++
		}
		if pending == 0 {
			return
		}
		select {
		case delay := <-results:
			pending--
			stopped = stopped || (delay > 0 && delay <= AndroidGoodDelay)
		case <-expand:
			limit = androidRecoveryConcurrency
			expand = nil
		case <-search:
			stopped = true
			search = nil
		}
	}
}
