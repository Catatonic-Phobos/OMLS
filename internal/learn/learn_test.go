package learn_test

import (
	"testing"

	"github.com/Catatonic-Phobos/OMLS/internal/learn"
	"github.com/Catatonic-Phobos/OMLS/internal/schedule"
)

func TestAdjustMovesTowardFasterNode(t *testing.T) {
	p := learn.New(0.5, 85)
	p.Observe("a", 100, 0, false)
	p.Observe("a", 100, 0, false)
	p.Observe("b", 400, 0, false)
	p.Observe("b", 400, 0, false)

	prev := schedule.Allocation{"a": 6, "b": 2}
	cap := map[string]int{"a": 8, "b": 8}
	next, note := p.Adjust(prev, cap, nil, nil)
	if next["a"] != 7 || next["b"] != 1 {
		t.Fatalf("alloc=%v note=%s", next, note)
	}
	if note == "" {
		t.Fatal("expected reason")
	}
}

func TestAdjustThermalEscape(t *testing.T) {
	p := learn.New(0.5, 70)
	p.Observe("hot", 50, 5, true)
	p.Observe("cool", 60, 1, true)
	prev := schedule.Allocation{"hot": 5, "cool": 3}
	cap := map[string]int{"hot": 8, "cool": 8}
	temp := map[string]float64{"hot": 80, "cool": 40}
	known := map[string]bool{"hot": true, "cool": true}
	next, note := p.Adjust(prev, cap, temp, known)
	if next["hot"] >= 5 {
		t.Fatalf("expected move off hot: %v (%s)", next, note)
	}
	if next["hot"]+next["cool"] != 8 {
		t.Fatalf("workers changed: %v", next)
	}
}

func TestAdjustMissingTempIsNeutral(t *testing.T) {
	p := learn.New(0.5, 85)
	p.Observe("a", 100, 0, false)
	p.Observe("b", 100, 0, false)
	prev := schedule.Allocation{"a": 4, "b": 4}
	next, note := p.Adjust(prev, map[string]int{"a": 4, "b": 4}, nil, nil)
	if next["a"] != 4 || next["b"] != 4 {
		t.Fatalf("%v %s", next, note)
	}
}
