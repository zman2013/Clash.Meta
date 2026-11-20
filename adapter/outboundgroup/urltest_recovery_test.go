package outboundgroup

import (
	"testing"
	"time"

	C "github.com/metacubex/mihomo/constant"
)

type recoveryProxy struct {
	C.Proxy
	name  string
	probe C.DelayHistory
}

func (p *recoveryProxy) Name() string                      { return p.name }
func (p *recoveryProxy) AliveForTestUrl(string) bool       { return true }
func (p *recoveryProxy) LastDelayForTestUrl(string) uint16 { return p.probe.Delay }
func (p *recoveryProxy) DelayHistoryForTestUrl(string) []C.DelayHistory {
	return []C.DelayHistory{p.probe}
}

func TestAndroidURLTestUsesFreshCandidateWithoutWaitingForIncumbent(t *testing.T) {
	now := time.Now()
	incumbent := &recoveryProxy{name: "old", probe: C.DelayHistory{Time: now.Add(-time.Minute), Delay: 80}}
	candidate := &recoveryProxy{name: "new", probe: C.DelayHistory{Time: now.Add(-time.Minute), Delay: 100}}
	proxies := []C.Proxy{incumbent, candidate}
	group := &URLTest{fastNode: incumbent, android: &androidURLTest{freshAfter: now}}
	if got := group.pickAndroid(proxies); got != incumbent {
		t.Fatal("discarded the working incumbent before a new result")
	}
	candidate.probe.Time = now.Add(time.Millisecond)
	if got := group.pickAndroid(proxies); got != candidate {
		t.Fatal("kept stale result when a fresh candidate became available")
	}
	group.selected = incumbent.Name()
	if got := group.pickAndroid(proxies); got != incumbent {
		t.Fatal("replaced the manually fixed node")
	}
}
