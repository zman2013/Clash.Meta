package provider

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
)

type probeTestProxy struct {
	C.Proxy
	name  string
	kind  C.AdapterType
	calls atomic.Int32
}

func (p *probeTestProxy) Name() string                      { return p.name }
func (p *probeTestProxy) Type() C.AdapterType               { return p.kind }
func (p *probeTestProxy) AliveForTestUrl(string) bool       { return true }
func (p *probeTestProxy) LastDelayForTestUrl(string) uint16 { return 10 }
func (p *probeTestProxy) URLTest(context.Context, string, utils.IntRanges[uint16]) (uint16, error) {
	p.calls.Add(1)
	return 10, nil
}

func TestAndroidHealthCheckLimitsActualProbeCalls(t *testing.T) {
	nodes := []*probeTestProxy{
		{name: "🇺🇸 Seattle", kind: C.Vmess},
		{name: "台灣 01", kind: C.Vmess},
		{name: "JP-02", kind: C.Vmess},
		{name: "Singapore 03", kind: C.Vmess},
		{name: "🇭🇰 Hong Kong", kind: C.Vmess},
		{name: "Business", kind: C.Vmess},
		{name: "美国组", kind: C.Selector},
	}
	var proxies []C.Proxy
	for _, node := range nodes {
		proxies = append(proxies, node)
	}
	hc := NewHealthCheck(proxies, "https://example.com/", 1000, 3, false, nil)
	defer hc.close()
	hc.androidPolicy = true
	start := time.Now()
	hc.checkAt(start)
	// Bypass the existing one-second result cache to exercise the Android
	// cooldown when a dial failure or provider update requests another round.
	hc.singleDo.Reset()
	hc.checkAt(start.Add(time.Minute))
	if delay := hc.nextAndroidRoundDelay(start.Add(time.Minute)); delay != 9*time.Minute {
		t.Fatalf("retry changed the scheduled round: next delay = %v", delay)
	}
	hc.checkAt(start.Add(10 * time.Minute))
	for i, node := range nodes {
		var want int32
		if i < 4 {
			want = 2
		}
		if got := node.calls.Load(); got != want {
			t.Errorf("%s: got %d probe calls, want %d", node.name, got, want)
		}
	}
}

func TestAndroidHealthCheckIntervalPreservesDisabledAndDesktopIntervals(t *testing.T) {
	if got := healthCheckInterval(3, true); got != 10*time.Minute {
		t.Fatalf("Android interval = %v", got)
	}
	if got := healthCheckInterval(0, true); got != 0 {
		t.Fatalf("disabled interval = %v", got)
	}
	if got := healthCheckInterval(3, false); got != 3*time.Second {
		t.Fatalf("desktop interval = %v", got)
	}
}
