package schedule_test

import (
	"testing"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
	"github.com/Catatonic-Phobos/OMLS/internal/schedule"
)

func TestPlanSplitsByCapacityAndLocality(t *testing.T) {
	nodes := []graph.NodeEntry{
		node("fast", "master-host", "kvm", 8, 0.1, false, 0),
		node("slow", "other", "wsl", 2, 1.5, false, 0),
	}
	alloc, cands, err := schedule.Plan(nodes, schedule.Request{Workers: 8}, schedule.Options{
		LocalHostname: "master-host",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 {
		t.Fatalf("cands=%d", len(cands))
	}
	if alloc["fast"]+alloc["slow"] != 8 {
		t.Fatalf("alloc=%v", alloc)
	}
	if alloc["fast"] <= alloc["slow"] {
		t.Fatalf("expected fast to get more workers: %v", alloc)
	}
}

func TestPlanThermalCeilingPenalizes(t *testing.T) {
	nodes := []graph.NodeEntry{
		node("hot", "a", "kvm", 8, 0.2, true, 90),
		node("cool", "b", "kvm", 8, 0.2, true, 50),
	}
	alloc, _, err := schedule.Plan(nodes, schedule.Request{Workers: 8}, schedule.Options{
		ThermalCeiling: 85,
	})
	if err != nil {
		t.Fatal(err)
	}
	if alloc["cool"] <= alloc["hot"] {
		t.Fatalf("cool should win: %v", alloc)
	}
}

func TestPlanRequiresCapabilities(t *testing.T) {
	n := node("x", "h", "bare", 4, 0, false, 0)
	n.Profile.Resources = []rdl.Resource{{
		ID: "disk0", Kind: "device", Class: "storage", Capabilities: []string{"storage"},
	}}
	_, _, err := schedule.Plan([]graph.NodeEntry{n}, schedule.Request{Workers: 2}, schedule.Options{})
	if err == nil {
		t.Fatal("expected no capable nodes")
	}
}

func node(id, host, virt string, cpus int, load float64, tempKnown bool, temp float64) graph.NodeEntry {
	doc := rdl.Empty()
	doc.Node = rdl.Node{ID: id, Hostname: host, OS: rdl.OSInfo{Family: "linux"}, Virt: virt}
	doc.Resources = []rdl.Resource{{
		ID: "cpu0", Kind: "device", Class: "compute",
		Capabilities: []string{"compute", "parallelizable"},
		Attrs:        map[string]any{"logical_cpus": cpus},
	}}
	n := graph.NodeEntry{
		ID: id, Hostname: host, Virt: virt, Status: graph.StatusAvailable,
		LastHeartbeat: time.Now(), Profile: &doc,
		Telemetry: &graph.Telemetry{CPULoad: load, TemperatureKnown: tempKnown, TemperatureC: temp},
	}
	return n
}
