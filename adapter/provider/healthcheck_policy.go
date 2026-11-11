package provider

import (
	"regexp"
	"time"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"
)

const androidHealthCheckInterval = 10 * time.Minute

var androidProbeRegion = regexp.MustCompile(`(?i)🇺🇸|🇹🇼|🇯🇵|🇸🇬|美国|美國|台湾|台灣|臺灣|日本|新加坡|狮城|獅城|\b(?:US|USA|United States|TW|Taiwan|JP|Japan|SG|Singapore)\b`)

func healthCheckInterval(seconds uint, android bool) time.Duration {
	if android && seconds > 0 {
		return androidHealthCheckInterval
	}
	return time.Duration(seconds) * time.Second
}

func isAndroidProbeCandidate(proxy C.Proxy) bool {
	// Groups can dial through members whose names have another region.
	switch proxy.Type() {
	case C.Direct, C.Reject, C.RejectDrop, C.Compatible, C.Pass, C.Dns,
		C.Relay, C.Selector, C.Fallback, C.URLTest, C.LoadBalance:
		return false
	}
	return androidProbeRegion.MatchString(proxy.Name())
}

func (hc *HealthCheck) beginRound(now time.Time) bool {
	if !hc.androidPolicy {
		return true
	}
	hc.mu.Lock()
	defer hc.mu.Unlock()
	if now.Before(hc.lastRoundStart.Add(androidHealthCheckInterval)) {
		return false
	}
	hc.lastRoundStart = now
	return true
}

// Recalculate the next timer after provider updates and failure-triggered checks
// so a round started just after the timer was armed does not cost a full cycle.
func (hc *HealthCheck) processAndroid() {
	hc.check()
	timer := time.NewTimer(hc.nextAndroidRoundDelay(time.Now()))
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			if !hc.lazy || time.Since(hc.lastTouch.Load()) < hc.interval {
				hc.check()
			} else {
				log.Debugln("Skip once health check because we are lazy")
			}
			timer.Reset(hc.nextAndroidRoundDelay(time.Now()))
		case <-hc.ctx.Done():
			return
		}
	}
}

func (hc *HealthCheck) nextAndroidRoundDelay(now time.Time) time.Duration {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	if delay := hc.lastRoundStart.Add(androidHealthCheckInterval).Sub(now); delay > 0 {
		return delay
	}
	return androidHealthCheckInterval
}
