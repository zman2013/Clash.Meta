package statistic

import C "github.com/metacubex/mihomo/constant"

type RequestNotify func(c Tracker)

type RequestFailureNotify func(metadata *C.Metadata, rule C.Rule, proxy C.ProxyAdapter, err error)

var DefaultRequestNotify RequestNotify
var DefaultRequestFailureNotify RequestFailureNotify

func (m *Manager) TotalTraffic(onlyProxy bool) (up, down int64) {
	if onlyProxy {
		return m.proxyUploadTotal.Load(), m.proxyDownloadTotal.Load()
	}
	return m.uploadTotal.Load(), m.downloadTotal.Load()
}

func (m *Manager) NowTraffic(onlyProxy bool) (up, down int64) {
	if onlyProxy {
		return m.proxyUploadBlip.Load(), m.proxyDownloadBlip.Load()
	}
	return m.uploadBlip.Load(), m.downloadBlip.Load()
}
