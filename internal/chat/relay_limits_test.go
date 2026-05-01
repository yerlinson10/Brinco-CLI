package chat

import (
	"net"
	"testing"
)

func TestRelayHubTryEnterLeavePerIP(t *testing.T) {
	h := &relayHub{
		maxPerIP: 2,
		ipCounts: make(map[string]int),
	}
	addr := &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 4444}

	ip, ok := h.tryEnter(addr)
	if !ok || ip != "10.0.0.1" {
		t.Fatalf("first tryEnter: ok=%v ip=%q", ok, ip)
	}
	if _, ok := h.tryEnter(addr); !ok {
		t.Fatal("second tryEnter should succeed")
	}
	if _, ok := h.tryEnter(addr); ok {
		t.Fatal("third tryEnter should fail (per-IP cap)")
	}
	h.leave("10.0.0.1")
	if _, ok := h.tryEnter(addr); !ok {
		t.Fatal("after leave one slot should open")
	}
}

func TestRelayHubTryEnterTotalCap(t *testing.T) {
	h := &relayHub{
		maxPerIP: 0,
		maxTotal: 2,
		ipCounts: make(map[string]int),
	}
	a1 := &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 1}
	a2 := &net.TCPAddr{IP: net.ParseIP("10.0.0.2"), Port: 2}

	if _, ok := h.tryEnter(a1); !ok {
		t.Fatal("first")
	}
	if _, ok := h.tryEnter(a2); !ok {
		t.Fatal("second")
	}
	if _, ok := h.tryEnter(a2); ok {
		t.Fatal("third should fail global cap")
	}
}

func TestRelayPeerIP(t *testing.T) {
	cases := []struct {
		addr net.Addr
		want string
	}{
		{&net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 99}, "2001:db8::1"},
		{&net.TCPAddr{IP: net.ParseIP("192.168.1.5"), Port: 10000}, "192.168.1.5"},
	}
	for _, tc := range cases {
		if got := relayPeerIP(tc.addr); got != tc.want {
			t.Errorf("relayPeerIP(%v) = %q want %q", tc.addr, got, tc.want)
		}
	}
}
