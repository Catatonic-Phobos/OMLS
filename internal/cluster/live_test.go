package cluster

import (
	"strings"
	"testing"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func TestUsageFromGraph(t *testing.T) {
	n := UsageFromGraph(graph.NodeEntry{
		Hostname: "omls",
		Status:   graph.StatusAvailable,
		Telemetry: &graph.Telemetry{
			CPULoad:           0.5,
			MemAvailableBytes: 1500,
			MemTotalBytes:     2000,
			TemperatureC:      30,
			TemperatureKnown:  true,
		},
		Profile: &rdl.Document{
			Resources: []rdl.Resource{{
				Class: "compute",
				Attrs: map[string]any{"logical_cpus": 2},
			}},
		},
	})
	if !n.CPUKnown || n.CPU < 24 || n.CPU > 26 {
		t.Fatalf("cpu %v known %v", n.CPU, n.CPUKnown)
	}
	if !n.RAMKnown || n.RAM < 24 || n.RAM > 26 || n.RAMUsed != 500 {
		t.Fatalf("ram %+v", n)
	}
	if n.Logical != 2 || !n.TempKnown {
		t.Fatalf("logical/temp %+v", n)
	}
}

func TestFormatLiveSplit(t *testing.T) {
	text := FormatLive(LiveView{
		At:          time.Date(2026, 10, 8, 22, 30, 0, 0, time.Local),
		Running:     true,
		Cluster:     "active",
		Coordinator: "omls",
		Nodes: []LiveNode{
			{Name: "Mintboy", CPU: 80, CPUKnown: true, BusyCores: 3.2, RAM: 50, RAMKnown: true, RAMUsed: 5000, Disk: 42, DiskKnown: true, GPU: 21, GPUKnown: true, Temp: 58, TempKnown: true},
			{Name: "omls", CPU: 25, CPUKnown: true, BusyCores: 0.5, RAM: 13, RAMKnown: true, RAMUsed: 250, Reserved: 512 << 20, Filled: 32 << 20, Temp: 30, TempKnown: true},
		},
	})
	for _, want := range []string{"OMLS: running", "Mintboy", "omls", "SPLIT", "512 MiB", "32 MiB", "100%", "reserve:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
}
