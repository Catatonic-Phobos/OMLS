package graph_test

import (
	"testing"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func TestRegistryHeartbeatTimeout(t *testing.T) {
	now := time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC)
	reg := graph.NewWithClock(50*time.Millisecond, func() time.Time { return now })
	reg.Register("n1", "host1", "kvm", "0.2.0", "sess")
	doc := sampleDoc("n1", "host1")
	if err := reg.Advertise("n1", "sess", &doc); err != nil {
		t.Fatal(err)
	}
	now = now.Add(20 * time.Millisecond)
	ok, _ := reg.Heartbeat("n1", "sess", &graph.Telemetry{CPULoad: 0.5, ObservedAt: now})
	if !ok {
		t.Fatal("heartbeat")
	}
	now = now.Add(100 * time.Millisecond)
	snap := reg.Snapshot()
	if len(snap.Nodes) != 1 || snap.Nodes[0].Status != graph.StatusUnavailable {
		t.Fatalf("%+v", snap.Nodes)
	}
	if snap.Nodes[0].Profile == nil || snap.Nodes[0].Profile.Node.ID != "n1" {
		t.Fatal("profile lost")
	}
}

func TestAdvertiseSessionMismatch(t *testing.T) {
	reg := graph.New(time.Second)
	reg.Register("n1", "h", "bare", "0.2.0", "a")
	doc := sampleDoc("n1", "h")
	if err := reg.Advertise("n1", "b", &doc); err == nil {
		t.Fatal("expected session mismatch")
	}
}

func sampleDoc(id, host string) rdl.Document {
	d := rdl.Empty()
	d.Node = rdl.Node{
		ID:       id,
		Hostname: host,
		OS:       rdl.OSInfo{Family: "linux", Arch: "x86_64"},
		Virt:     "kvm",
	}
	d.Resources = []rdl.Resource{{
		ID: "cpu0", Kind: "device", Class: "compute",
		Capabilities: []string{"compute"},
	}}
	return d
}
