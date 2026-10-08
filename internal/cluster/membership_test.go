package cluster

import (
	"testing"
	"time"
)

func TestElectionFollowsLowestReadyID(t *testing.T) {
	m := NewMembership(time.Second, Member{
		NodeID: "b-node", Hostname: "DESKTOP", OS: "Debian 13", Virt: "wsl",
	})
	m.Upsert(Member{NodeID: "c-node", Hostname: "OTHER", Virt: "bare", Addr: "192.168.15.20:7443"})
	m.Upsert(Member{NodeID: "a-node", Hostname: "MINT", OS: "Linux Mint", Virt: "bare", Addr: "192.168.15.14:7443"})

	coord, ok := m.Coordinator()
	if !ok || coord.NodeID != "a-node" {
		t.Fatalf("coordinator=%+v ok=%v", coord, ok)
	}

	m.MarkUnavailable("a-node")
	coord, ok = m.Coordinator()
	if !ok || coord.NodeID != "b-node" {
		t.Fatalf("after loss coordinator=%+v ok=%v", coord, ok)
	}

	snap := m.Snapshot()
	var found bool
	for _, member := range snap {
		if member.NodeID == "a-node" {
			found = true
			if member.Status != StatusUnavailable {
				t.Fatalf("status=%s", member.Status)
			}
		}
	}
	if !found {
		t.Fatal("unavailable member was dropped")
	}

	m.Upsert(Member{NodeID: "a-node", Hostname: "MINT", OS: "Linux Mint", Virt: "bare", Addr: "192.168.15.14:7443"})
	coord, ok = m.Coordinator()
	if !ok || coord.NodeID != "a-node" {
		t.Fatalf("rejoin coordinator=%+v", coord)
	}
	for _, member := range m.Snapshot() {
		if member.NodeID == "a-node" && member.Status != StatusReady {
			t.Fatalf("rejoin status=%s", member.Status)
		}
	}
}

func TestSweepMarksSilenceUnavailable(t *testing.T) {
	m := NewMembership(10*time.Millisecond, Member{NodeID: "self", Hostname: "DESKTOP"})
	m.Upsert(Member{
		NodeID: "peer", Hostname: "MINT", LastSeen: time.Now().Add(-time.Second),
	})
	m.Sweep(time.Now())
	for _, member := range m.Snapshot() {
		if member.NodeID == "peer" && member.Status != StatusUnavailable {
			t.Fatalf("status=%s", member.Status)
		}
		if member.NodeID == "self" && member.Status != StatusReady {
			t.Fatalf("self status=%s", member.Status)
		}
	}
}

func TestFormatNodes(t *testing.T) {
	text := FormatNodes([]NodeInfo{
		{Name: "DESKTOP", OS: "Debian 13", Env: EnvLabel("wsl"), Status: StatusJoined},
		{Name: "MINT", OS: "Linux Mint", Env: EnvLabel("bare"), Status: StatusDiscovered},
	})
	for _, want := range []string{"NODE", "DESKTOP", "Debian 13", "WSL2", "MINT", "native", "joined", "discovered"} {
		if !contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
	status := FormatStatus(Status{Running: true, Cluster: "active", Nodes: 2, Joined: 1, Discovered: 1, Coordinator: "DESKTOP"})
	for _, want := range []string{"OMLS: running", "Cluster: active", "joined=1", "discovered=1", "Coordinator: DESKTOP"} {
		if !contains(status, want) {
			t.Fatalf("missing %q in\n%s", want, status)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})())
}
