package learn_test

import (
	"strings"
	"testing"

	"github.com/Catatonic-Phobos/OMLS/internal/learn"
	"github.com/Catatonic-Phobos/OMLS/internal/schedule"
)

func TestNormalizePolicy(t *testing.T) {
	if learn.NormalizePolicy("") != learn.PolicyEWMA {
		t.Fatal("default")
	}
	if learn.NormalizePolicy("adaptive") != learn.PolicyAdaptive {
		t.Fatal("adaptive")
	}
	if learn.NormalizePolicy("MULTI-SIGNAL") != learn.PolicyAdaptive {
		t.Fatal("alias")
	}
}

func TestScoreNodePrefersFasterCooler(t *testing.T) {
	opt := learn.AdaptiveOptions{Weights: learn.DefaultAdaptiveWeights(), ThermalCeiling: 85}
	fast := learn.Stats{DurationEWMA: 80, Samples: 5, TempDeltaEWMA: 1}
	slow := learn.Stats{DurationEWMA: 400, Samples: 5, TempDeltaEWMA: 8}
	sigFast := learn.NodeSignals{Load: 0.2, LoadKnown: true, TempC: 45, TempKnown: true, Capacity: 8, IsLocal: true}
	sigSlow := learn.NodeSignals{Load: 1.5, LoadKnown: true, TempC: 78, TempKnown: true, Capacity: 8, Virt: "wsl"}
	sf, _ := learn.ScoreNode(fast, sigFast, opt)
	ss, _ := learn.ScoreNode(slow, sigSlow, opt)
	if sf <= ss {
		t.Fatalf("expected fast/cool > slow/hot: %.1f vs %.1f", sf, ss)
	}
}

func TestAdaptiveAdjustMovesTowardBetterScore(t *testing.T) {
	p := learn.New(0.5, 85)
	p.Observe("a", 80, 1, true)
	p.Observe("a", 80, 1, true)
	p.Observe("b", 400, 5, true)
	p.Observe("b", 400, 5, true)

	prev := schedule.Allocation{"a": 4, "b": 4}
	signals := map[string]learn.NodeSignals{
		"a": {Load: 0.3, LoadKnown: true, TempC: 40, TempKnown: true, Capacity: 8, IsLocal: true},
		"b": {Load: 2.0, LoadKnown: true, TempC: 70, TempKnown: true, Capacity: 8, Virt: "wsl"},
	}
	opt := learn.AdaptiveOptions{
		Weights:          learn.DefaultAdaptiveWeights(),
		ThermalCeiling:   85,
		CooldownRounds:    1, // allow immediate first move
		MinScoreDelta:    0.05,
		MaxMovesPerRound: 1,
		Round:            2,
	}
	next, note := p.AdaptiveAdjust(prev, signals, opt)
	if next["a"] <= 4 {
		t.Fatalf("expected move toward a: %v note=%s", next, note)
	}
	if next["a"]+next["b"] != 8 {
		t.Fatalf("workers changed: %v", next)
	}
	if !strings.Contains(note, "adaptive") {
		t.Fatalf("note=%s", note)
	}
}

func TestAdaptiveCooldownHysteresis(t *testing.T) {
	p := learn.New(0.5, 85)
	p.Observe("a", 50, 0, false)
	p.Observe("b", 300, 0, false)
	signals := map[string]learn.NodeSignals{
		"a": {LoadKnown: true, Load: 0.1, Capacity: 8},
		"b": {LoadKnown: true, Load: 0.1, Capacity: 8},
	}
	opt := learn.AdaptiveOptions{
		Weights:          learn.DefaultAdaptiveWeights(),
		CooldownRounds:    3,
		MinScoreDelta:    0.05,
		MaxMovesPerRound: 1,
		Round:            2,
	}
	prev := schedule.Allocation{"a": 4, "b": 4}
	next1, note1 := p.AdaptiveAdjust(prev, signals, opt)
	if next1["a"] == 4 {
		t.Fatalf("expected first move: %v %s", next1, note1)
	}
	opt.Round = 3 // still within cooldown (last_move=2, cooldown=3 → hold while round-last < 3)
	next2, note2 := p.AdaptiveAdjust(next1, signals, opt)
	if next2["a"] != next1["a"] || next2["b"] != next1["b"] {
		t.Fatalf("expected cooldown hold: before=%v after=%v note=%s", next1, next2, note2)
	}
	if !strings.Contains(note2, "cooldown") {
		t.Fatalf("expected cooldown note: %s", note2)
	}
}

func TestAdaptiveThermalOverridesCooldown(t *testing.T) {
	p := learn.New(0.5, 70)
	p.Observe("hot", 50, 5, true)
	p.Observe("cool", 60, 1, true)
	signals := map[string]learn.NodeSignals{
		"hot":  {TempC: 80, TempKnown: true, Capacity: 8, LoadKnown: true},
		"cool": {TempC: 40, TempKnown: true, Capacity: 8, LoadKnown: true},
	}
	// Force last move so cooldown would hold, then thermal must still escape.
	prev := schedule.Allocation{"hot": 5, "cool": 3}
	opt := learn.AdaptiveOptions{
		Weights:        learn.DefaultAdaptiveWeights(),
		ThermalCeiling: 70,
		CooldownRounds:  5,
		Round:          2,
	}
	// Prime hysteresis
	_, _ = p.AdaptiveAdjust(prev, map[string]learn.NodeSignals{
		"hot":  {TempC: 50, TempKnown: true, Capacity: 8, Load: 0, LoadKnown: true},
		"cool": {TempC: 40, TempKnown: true, Capacity: 8, Load: 0, LoadKnown: true},
	}, learn.AdaptiveOptions{Weights: learn.DefaultAdaptiveWeights(), CooldownRounds: 1, MinScoreDelta: 0.01, Round: 1, ThermalCeiling: 70})

	opt.Round = 2
	next, note := p.AdaptiveAdjust(prev, signals, opt)
	if next["hot"] >= 5 {
		t.Fatalf("thermal should move off hot: %v (%s)", next, note)
	}
	if !strings.Contains(note, "thermal") {
		t.Fatalf("note=%s", note)
	}
}

func TestEnvelopeCostFromYAML(t *testing.T) {
	if c := learn.EnvelopeCostFromYAML(nil); c != 0 {
		t.Fatalf("nil=%v", c)
	}
	eco := []byte("name: eco\nperformance: eco\nsustain:\n  level: 0.35\n")
	if c := learn.EnvelopeCostFromYAML(eco); c < 0.3 || c > 0.4 {
		t.Fatalf("eco cost=%v", c)
	}
	high := []byte("performance: high\n")
	if c := learn.EnvelopeCostFromYAML(high); c < 0.8 {
		t.Fatalf("high cost=%v", c)
	}
}
