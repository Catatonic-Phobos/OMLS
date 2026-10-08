package cluster

import (
	"context"
	"testing"
	"time"
)

func TestMDNSSelfDiscover(t *testing.T) {
	dir := NewMDNSDirectory()
	dir.Interval = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	self := Peer{
		NodeID:          "omls-mdns-test",
		Hostname:        "SELF",
		OS:              "test",
		Virt:            "bare",
		Host:            "127.0.0.1",
		Port:            17443,
		ProtocolVersion: ProtocolVersion,
	}
	if err := dir.Announce(ctx, self); err != nil {
		t.Skipf("mDNS announce unavailable: %v", err)
	}
	events, err := dir.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(7 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("discovery closed before the local node was seen")
			}
			if ev.Peer.NodeID == self.NodeID && ev.Peer.Port == self.Port {
				if ev.Peer.Host == "" {
					t.Fatalf("peer missing address: %+v", ev.Peer)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for the local mDNS advertisement")
		}
	}
}
