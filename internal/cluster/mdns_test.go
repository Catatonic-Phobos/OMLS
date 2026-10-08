package cluster

import (
	"net"
	"testing"

	"github.com/grandcat/zeroconf"
)

func TestPeerFromEntryUsesAdvertisedAddr(t *testing.T) {
	e := zeroconf.NewServiceEntry("ignored", ServiceType, "local")
	e.Port = 7443
	e.Text = []string{
		"node_id=abc123",
		"proto=1",
		"hostname=MINT",
		"os=Linux Mint",
		"virt=bare",
		"addr=192.168.15.14",
	}
	e.AddrIPv4 = []net.IP{net.ParseIP("10.0.0.8").To4()}
	p, ok := peerFromEntry(e)
	if !ok {
		t.Fatal("expected peer")
	}
	if p.NodeID != "abc123" || p.Host != "192.168.15.14" || p.Port != 7443 {
		t.Fatalf("peer=%+v", p)
	}
	if p.Hostname != "MINT" || p.OS != "Linux Mint" || p.Virt != "bare" {
		t.Fatalf("meta=%+v", p)
	}
}

func TestPeerFromEntryRejectsEmpty(t *testing.T) {
	if _, ok := peerFromEntry(nil); ok {
		t.Fatal("nil entry")
	}
	e := zeroconf.NewServiceEntry("x", ServiceType, "local")
	if _, ok := peerFromEntry(e); ok {
		t.Fatal("entry without address")
	}
}
