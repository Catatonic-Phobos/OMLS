package fabric

import (
	"testing"

	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
	"github.com/Catatonic-Phobos/OMLS/internal/schedule"
)

func TestGPUWorkUnitsFollowDiscoveredDeviceOrder(t *testing.T) {
	nodes := []graph.NodeEntry{{
		ID: "node-a",
		Profile: &rdl.Document{Resources: []rdl.Resource{
			{ID: "gpu0", Class: "graphics", Attrs: map[string]any{"compute_backend": "opencl", "compute_device_index": 1}},
			{ID: "gpu1", Class: "graphics", Attrs: map[string]any{"compute_backend": "opencl", "compute_device_index": 0}},
		}},
	}}
	indices := openCLIndices(nodes)
	units := enqueueAlloc(schedule.Allocation{"node-a": 2}, 30, 2, nil, "gpu_serial", true, indices)
	if len(units) != 2 {
		t.Fatalf("units=%d, want 2", len(units))
	}
	if units[0].Index != 1 || units[1].Index != 0 {
		t.Fatalf("device execution order=%d,%d, want discovered OpenCL indices 1,0", units[0].Index, units[1].Index)
	}
	if units[0].Function != "gpu_serial" || units[1].Function != "gpu_serial" {
		t.Fatalf("functions=%q,%q", units[0].Function, units[1].Function)
	}
}
