package schedule_test

import (
	"testing"

	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/schedule"
)

func TestPlaceExclusiveSeparatesFunctions(t *testing.T) {
	nodes := []graph.NodeEntry{
		node("a", "host-a", "bare", 8, 0.1, false, 0),
		node("b", "host-b", "bare", 4, 0.2, false, 0),
	}
	placements, err := schedule.PlaceExclusive(nodes, []schedule.FuncRequest{
		{Function: "parallel_workers", Requires: []string{"compute", "parallelizable"}, Workers: 2},
		{Function: "memory_touch", Requires: []string{"compute", "parallelizable"}, Workers: 2},
	}, schedule.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(placements) != 2 {
		t.Fatalf("placements=%d", len(placements))
	}
	if placements[0].NodeID == placements[1].NodeID {
		t.Fatalf("expected distinct nodes, got %+v", placements)
	}
	seen := map[string]bool{}
	for _, p := range placements {
		if seen[p.Function] {
			t.Fatalf("duplicate function %s", p.Function)
		}
		seen[p.Function] = true
		if p.Workers < 1 {
			t.Fatalf("workers=%d for %s", p.Workers, p.Function)
		}
	}
}

func TestPlaceExclusiveReusesWhenOnlyOneNode(t *testing.T) {
	nodes := []graph.NodeEntry{
		node("solo", "host", "bare", 8, 0.1, false, 0),
	}
	placements, err := schedule.PlaceExclusive(nodes, []schedule.FuncRequest{
		{Function: "parallel_workers", Workers: 1},
		{Function: "memory_touch", Workers: 1},
	}, schedule.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(placements) != 2 {
		t.Fatalf("placements=%d", len(placements))
	}
	if placements[0].NodeID != "solo" || placements[1].NodeID != "solo" {
		t.Fatalf("expected reuse on solo: %+v", placements)
	}
}
