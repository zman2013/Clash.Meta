package statistic

import (
	"net"
	"testing"

	"github.com/metacubex/mihomo/common/buf"
	C "github.com/metacubex/mihomo/constant"
)

type trafficTestConn struct{ C.Conn }

func (trafficTestConn) Chains() C.Chain                 { return C.Chain{"Node", "Proxy"} }
func (trafficTestConn) ProviderChains() C.Chain         { return nil }
func (trafficTestConn) RemoteDestination() string       { return "" }
func (trafficTestConn) Read(b []byte) (int, error)      { return copy(b, []byte("abc")), nil }
func (trafficTestConn) Write(b []byte) (int, error)     { return len(b), nil }
func (trafficTestConn) ReadBuffer(b *buf.Buffer) error  { _, err := b.Write([]byte("abc")); return err }
func (trafficTestConn) WriteBuffer(b *buf.Buffer) error { return nil }
func (trafficTestConn) Close() error                    { return nil }

type trafficTestPacketConn struct{ C.PacketConn }

func (trafficTestPacketConn) Chains() C.Chain           { return C.Chain{"Node", "Proxy"} }
func (trafficTestPacketConn) ProviderChains() C.Chain   { return nil }
func (trafficTestPacketConn) RemoteDestination() string { return "" }
func (trafficTestPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	return copy(b, []byte("abc")), nil, nil
}
func (trafficTestPacketConn) WaitReadFrom() ([]byte, func(), net.Addr, error) {
	return []byte("abcd"), func() {}, nil, nil
}
func (trafficTestPacketConn) WriteTo(b []byte, a net.Addr) (int, error) { return len(b), nil }
func (trafficTestPacketConn) Close() error                              { return nil }

func TestTrafficNotifyAllPayloadPathsWithoutPolling(t *testing.T) {
	old := DefaultTrafficNotify
	defer func() { DefaultTrafficNotify = old }()
	var up, down int64
	DefaultTrafficNotify = func(info *TrackerInfo, u, d int64) { up += u; down += d }
	manager := &Manager{}
	tcp := NewTCPTracker(trafficTestConn{}, manager, &C.Metadata{Process: "app"}, nil, 1, 2, true)
	_, _ = tcp.Read(make([]byte, 10))
	_, _ = tcp.Write([]byte("hello"))
	buffer := buf.NewSize(32)
	defer buffer.Release()
	_ = tcp.ReadBuffer(buffer)
	_ = tcp.WriteBuffer(buffer)
	_, readCounts := tcp.UnwrapReader()
	readCounts[0](11)
	_, writeCounts := tcp.UnwrapWriter()
	writeCounts[0](13)
	_ = tcp.Close()
	if up != 22 || down != 19 {
		t.Fatalf("TCP payload lost: up=%d down=%d", up, down)
	}
	udp := NewUDPTracker(trafficTestPacketConn{}, manager, &C.Metadata{Process: "app"}, nil, 1, 2, true)
	_, _, _ = udp.ReadFrom(make([]byte, 10))
	_, _, _, _ = udp.WaitReadFrom()
	_, _ = udp.WriteTo([]byte("hello"), nil)
	_ = udp.Close()
	if up != 28 || down != 28 {
		t.Fatalf("UDP payload lost: up=%d down=%d", up, down)
	}
	nested := NewTCPTracker(trafficTestConn{}, manager, &C.Metadata{}, nil, 100, 100, false)
	_, _ = nested.Write([]byte("hello"))
	_, _ = nested.Read(make([]byte, 10))
	_ = nested.Close()
	if up != 28 || down != 28 {
		t.Fatal("nested payload double counted")
	}
	totalUp, totalDown := manager.Total()
	if totalUp != up || totalDown != down {
		t.Fatal("observer disagrees with core counters")
	}
}
