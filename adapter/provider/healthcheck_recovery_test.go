package provider

import (
	"testing"
	"time"

	C "github.com/metacubex/mihomo/constant"
)

type recentProbeProxy struct {
	probeTestProxy
	history []C.DelayHistory
}

func (p *recentProbeProxy) DelayHistoryForTestUrl(string) []C.DelayHistory { return p.history }

func TestAndroidProbeCandidatesPrioritizeRecentReliableNodes(t *testing.T) {
	now := time.Now()
	makeProxy := func(name string, age time.Duration, delays ...uint16) *recentProbeProxy {
		proxy := &recentProbeProxy{probeTestProxy: probeTestProxy{name: name, kind: C.Vmess}}
		for _, delay := range delays {
			proxy.history = append(proxy.history, C.DelayHistory{Time: now.Add(-age), Delay: delay})
		}
		return proxy
	}
	failing := makeProxy("US slow", time.Minute, 0, 1600, 0)
	unknown := makeProxy("TW unknown", 3*time.Hour, 2, 2, 2)
	fast := makeProxy("SG fast", time.Minute, 150, 100, 120)
	unstable := makeProxy("JP unstable", time.Minute, 60, 50, 0)
	outsideRegion := makeProxy("HK good", time.Minute, 20, 20)
	proxies := []C.Proxy{failing, unknown, fast, outsideRegion, unstable}
	got := androidProbeCandidates(proxies, "https://example.com/", now)
	want := []C.Proxy{fast, unstable, unknown, failing}
	if len(got) != len(want) {
		t.Fatalf("got %d nodes, want %d", len(got), len(want))
	}
	for i, proxy := range got {
		if proxy != want[i] {
			t.Errorf("rank %d = %s, want %s", i, proxy.Name(), want[i].Name())
		}
	}
	if proxies[0] != failing {
		t.Fatal("ranking modified provider order")
	}
}

func TestAndroidRecoveryWidensSearchAndStopsSchedulingAfterGoodResult(t *testing.T) {
	var candidates []C.Proxy
	for i := 0; i < 20; i++ {
		candidates = append(candidates, &probeTestProxy{})
	}
	expand := make(chan time.Time)
	results := make(chan uint16, androidRecoveryConcurrency)
	started := make(chan C.Proxy, len(candidates))
	done := make(chan struct{})
	go func() {
		defer close(done)
		recoverAndroidProxies(candidates, expand, nil, results, func(p C.Proxy) { started <- p })
	}()
	read := func(i int) {
		t.Helper()
		select {
		case got := <-started:
			if got != candidates[i] {
				t.Fatalf("probe %d started out of order", i)
			}
		case <-time.After(time.Second):
			t.Fatalf("probe %d was not started", i)
		}
	}
	for i := 0; i < androidRecoveryShortlist; i++ {
		read(i)
	}
	select {
	case expand <- time.Now():
	case <-started:
		t.Fatal("started more than the shortlist before expansion")
	case <-time.After(time.Second):
		t.Fatal("expansion signal was not received")
	}
	for i := androidRecoveryShortlist; i < androidRecoveryConcurrency; i++ {
		read(i)
	}
	results <- 200
	for i := 1; i < androidRecoveryConcurrency; i++ {
		results <- 0
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recovery did not finish after the active probes drained")
	}
	if len(started) != 0 {
		t.Fatal("started extra probes after a good result")
	}
}
