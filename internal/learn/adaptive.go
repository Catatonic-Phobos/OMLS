// Adaptive multi-signal scheduling (OMLS 0.6).
package learn

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/schedule"
)

// Policy names for Adjust / AdaptiveAdjust.
const (
	PolicyEWMA     = "ewma"     // 0.3 thermal + duration EWMA rules
	PolicyAdaptive = "adaptive" // 0.6 multi-signal + hysteresis
)

// NormalizePolicy returns a known policy name (default ewma).
func NormalizePolicy(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", PolicyEWMA, "legacy", "simple":
		return PolicyEWMA
	case PolicyAdaptive, "multi", "multisignal", "multi-signal":
		return PolicyAdaptive
	default:
		return PolicyEWMA
	}
}

// AdaptiveWeights are explainable blend coefficients for multi-signal scoring.
// Higher weight → more influence. Zero weight disables that signal.
type AdaptiveWeights struct {
	Duration  float64 // prefer lower duration EWMA
	Load      float64 // prefer lower CPU load
	Thermal   float64 // prefer cooler / under ceiling
	Locality  float64 // prefer local / non-WSL
	Envelope  float64 // prefer nodes that absorb envelope intensity better
	Capacity  float64 // slight preference for spare capacity
}

// DefaultAdaptiveWeights returns a balanced explainable blend.
func DefaultAdaptiveWeights() AdaptiveWeights {
	return AdaptiveWeights{
		Duration: 1.0,
		Load:     0.6,
		Thermal:  0.8,
		Locality: 0.4,
		Envelope: 0.3,
		Capacity: 0.2,
	}
}

// AdaptiveOptions tunes hysteresis and scoring for PolicyAdaptive.
type AdaptiveOptions struct {
	Weights          AdaptiveWeights
	ThermalCeiling   float64
	WSLPenalty       float64 // subtracted from locality component
	CooldownRounds    int     // rounds to hold after a move (default 2)
	MinScoreDelta    float64 // relative score gap required to move (default 0.12)
	MaxMovesPerRound int     // default 1
	EnvelopeCost     float64 // 0..1 intensity hint from envelope (higher = more demanding)
	Round            int     // current demo round (1-based); used for cooldown
}

// NodeSignals are optional live + learned inputs for one node.
type NodeSignals struct {
	Load       float64
	LoadKnown  bool
	TempC      float64
	TempKnown  bool
	Virt       string
	IsLocal    bool
	Capacity   int
}

// AdaptiveState tracks hysteresis across Adjust calls on one Plane.
type AdaptiveState struct {
	LastMoveRound int
	LastNote      string
}

// adaptive is lazily allocated on the Plane.
func (p *Plane) ensureAdaptive() *AdaptiveState {
	if p.adaptive == nil {
		p.adaptive = &AdaptiveState{}
	}
	return p.adaptive
}

// ScoreNode blends multi-signal inputs into a single preference score (higher = better)
// plus an explainable reason string.
func ScoreNode(st Stats, sig NodeSignals, opt AdaptiveOptions) (float64, string) {
	w := opt.Weights
	if w == (AdaptiveWeights{}) {
		w = DefaultAdaptiveWeights()
	}
	if opt.ThermalCeiling <= 0 {
		opt.ThermalCeiling = 85
	}
	if opt.WSLPenalty == 0 {
		opt.WSLPenalty = 2
	}

	parts := []string{}
	score := 50.0 // baseline

	// Duration: invert EWMA — faster nodes score higher.
	if w.Duration > 0 && st.DurationEWMA > 0 && st.Samples > 0 {
		// Map ~50–500ms into a useful range; lower duration → higher contribution.
		durComp := 100.0 / (1.0 + st.DurationEWMA/100.0)
		score += w.Duration * durComp
		parts = append(parts, fmt.Sprintf("dur=%.0fms*%.1f→%.1f", st.DurationEWMA, w.Duration, w.Duration*durComp))
	} else if w.Duration > 0 {
		parts = append(parts, "dur=unknown")
	}

	// Load: lower load preferred.
	if w.Load > 0 {
		if sig.LoadKnown {
			loadComp := math.Max(0, 20-sig.Load*10)
			score += w.Load * loadComp
			parts = append(parts, fmt.Sprintf("load=%.2f*%.1f→%.1f", sig.Load, w.Load, w.Load*loadComp))
		} else {
			parts = append(parts, "load=unknown")
		}
	}

	// Thermal: under ceiling preferred; over ceiling heavily penalized.
	if w.Thermal > 0 {
		if sig.TempKnown {
			tempComp := 0.0
			if sig.TempC >= opt.ThermalCeiling {
				tempComp = -40
				parts = append(parts, fmt.Sprintf("temp=%.1f≥ceiling%.0f→%.1f", sig.TempC, opt.ThermalCeiling, w.Thermal*tempComp))
			} else {
				// Cooler relative to ceiling → higher.
				headroom := opt.ThermalCeiling - sig.TempC
				tempComp = math.Min(25, headroom/2)
				if st.TempDeltaEWMA > 0 {
					tempComp -= math.Min(10, st.TempDeltaEWMA)
				}
				parts = append(parts, fmt.Sprintf("temp=%.1fΔ%.1f*%.1f→%.1f", sig.TempC, st.TempDeltaEWMA, w.Thermal, w.Thermal*tempComp))
			}
			score += w.Thermal * tempComp
		} else {
			parts = append(parts, "temp=unknown")
		}
	}

	// Locality / virt.
	if w.Locality > 0 {
		loc := 0.0
		if sig.IsLocal {
			loc += 8
			parts = append(parts, "locality=local")
		} else {
			loc -= 1
			parts = append(parts, "locality=remote")
		}
		virt := strings.ToLower(sig.Virt)
		if virt == "wsl" {
			loc -= opt.WSLPenalty
			parts = append(parts, fmt.Sprintf("virt=wsl-%.0f", opt.WSLPenalty))
		} else if virt != "" {
			parts = append(parts, "virt="+virt)
		}
		score += w.Locality * loc
	}

	// Envelope cost: demanding envelopes prefer cooler/faster nodes (already in mix);
	// also slight penalty on high-load nodes when cost is high.
	if w.Envelope > 0 && opt.EnvelopeCost > 0 {
		pen := opt.EnvelopeCost * 10
		if sig.LoadKnown {
			pen += opt.EnvelopeCost * sig.Load * 5
		}
		score -= w.Envelope * pen
		parts = append(parts, fmt.Sprintf("envelope_cost=%.2f→-%.1f", opt.EnvelopeCost, w.Envelope*pen))
	}

	// Spare capacity preference.
	if w.Capacity > 0 && sig.Capacity > 0 {
		capComp := float64(sig.Capacity) * 0.5
		score += w.Capacity * capComp
		parts = append(parts, fmt.Sprintf("cap=%d→%.1f", sig.Capacity, w.Capacity*capComp))
	}

	if score < 0.1 {
		score = 0.1
	}
	return score, strings.Join(parts, " ")
}

// AdaptiveAdjust rebalances using multi-signal scores with hysteresis/cooldown.
func (p *Plane) AdaptiveAdjust(prev schedule.Allocation, signals map[string]NodeSignals, opt AdaptiveOptions) (schedule.Allocation, string) {
	if opt.CooldownRounds <= 0 {
		opt.CooldownRounds = 2
	}
	if opt.MinScoreDelta <= 0 {
		opt.MinScoreDelta = 0.12
	}
	if opt.MaxMovesPerRound <= 0 {
		opt.MaxMovesPerRound = 1
	}
	if opt.ThermalCeiling <= 0 {
		opt.ThermalCeiling = p.ThermalCeiling
	}
	if opt.Weights == (AdaptiveWeights{}) {
		opt.Weights = DefaultAdaptiveWeights()
	}

	next := schedule.Allocation{}
	ids := make([]string, 0, len(prev))
	total := 0
	for id, w := range prev {
		next[id] = w
		ids = append(ids, id)
		total += w
	}
	sort.Strings(ids)
	if len(ids) < 2 || total == 0 {
		return next, "adaptive: no rebalance (fewer than 2 nodes with work)"
	}

	st := p.ensureAdaptive()
	scores := map[string]float64{}
	reasons := map[string]string{}
	for _, id := range ids {
		sig := signals[id]
		nodeStats := Stats{}
		if s := p.nodes[id]; s != nil {
			nodeStats = *s
		}
		sc, why := ScoreNode(nodeStats, sig, opt)
		scores[id] = sc
		reasons[id] = why
	}

	// Always allow thermal escape even during cooldown.
	thermalNotes := []string{}
	for _, id := range ids {
		sig := signals[id]
		if !sig.TempKnown || sig.TempC < opt.ThermalCeiling || next[id] <= 0 {
			continue
		}
		recv := bestAdaptiveReceiver(ids, next, signals, scores, id)
		if recv == "" {
			thermalNotes = append(thermalNotes, fmt.Sprintf("thermal: %s over ceiling but no receiver", short(id)))
			continue
		}
		next[id]--
		next[recv]++
		st.LastMoveRound = opt.Round
		thermalNotes = append(thermalNotes, fmt.Sprintf("thermal: move 1 %s→%s (temp %.1f≥%.0f)", short(id), short(recv), sig.TempC, opt.ThermalCeiling))
	}
	if len(thermalNotes) > 0 {
		note := "adaptive|" + strings.Join(thermalNotes, "; ")
		st.LastNote = note
		return next, note
	}

	// Hysteresis: hold if we moved recently.
	if st.LastMoveRound > 0 && opt.Round > 0 && opt.Round-st.LastMoveRound < opt.CooldownRounds {
		hold := fmt.Sprintf("adaptive: cooldown hold (last_move_round=%d round=%d cooldown=%d)", st.LastMoveRound, opt.Round, opt.CooldownRounds)
		// Include top scores for explainability.
		hold += "; " + formatTopScores(ids, scores, reasons)
		st.LastNote = hold
		return next, hold
	}

	// Find best and worst scored nodes that participate.
	bestID, worstID := "", ""
	bestSc, worstSc := -1e9, 1e9
	for _, id := range ids {
		if next[id] == 0 && prev[id] == 0 {
			continue
		}
		sc := scores[id]
		if sc > bestSc {
			bestSc = sc
			bestID = id
		}
		if sc < worstSc && next[id] > 0 {
			worstSc = sc
			worstID = id
		}
	}
	if bestID == "" || worstID == "" || bestID == worstID {
		note := "adaptive: hold (no score spread); " + formatTopScores(ids, scores, reasons)
		st.LastNote = note
		return next, note
	}

	// Relative delta: (best-worst)/max(best,1)
	rel := (bestSc - worstSc) / math.Max(bestSc, 1)
	if rel < opt.MinScoreDelta {
		note := fmt.Sprintf("adaptive: hold (Δscore=%.3f < min=%.3f); %s", rel, opt.MinScoreDelta, formatTopScores(ids, scores, reasons))
		st.LastNote = note
		return next, note
	}

	moves := 0
	notes := []string{}
	for moves < opt.MaxMovesPerRound {
		capRecv := signals[bestID].Capacity
		if capRecv > 0 && next[bestID] >= capRecv {
			notes = append(notes, fmt.Sprintf("blend: prefer %s but at capacity", short(bestID)))
			break
		}
		if next[worstID] <= 0 {
			break
		}
		next[worstID]--
		next[bestID]++
		moves++
		notes = append(notes, fmt.Sprintf("blend: move 1 %s→%s (score %.1f→%.1f Δ=%.3f)", short(worstID), short(bestID), worstSc, bestSc, rel))
	}
	if moves == 0 {
		note := "adaptive: hold (capacity/clamp); " + formatTopScores(ids, scores, reasons)
		st.LastNote = note
		return next, note
	}
	st.LastMoveRound = opt.Round
	note := "adaptive|" + strings.Join(notes, "; ") + "; " + formatTopScores(ids, scores, reasons)
	st.LastNote = note
	return next, note
}

func bestAdaptiveReceiver(ids []string, alloc schedule.Allocation, signals map[string]NodeSignals, scores map[string]float64, donor string) string {
	best := ""
	bestSc := -1e9
	for _, id := range ids {
		if id == donor {
			continue
		}
		cap := signals[id].Capacity
		if cap > 0 && alloc[id] >= cap {
			continue
		}
		sc := scores[id]
		if sc > bestSc {
			bestSc = sc
			best = id
		}
	}
	return best
}

func formatTopScores(ids []string, scores map[string]float64, reasons map[string]string) string {
	type row struct {
		id string
		sc float64
	}
	rows := make([]row, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, row{id: id, sc: scores[id]})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].sc == rows[j].sc {
			return rows[i].id < rows[j].id
		}
		return rows[i].sc > rows[j].sc
	})
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, fmt.Sprintf("%s=%.1f[%s]", short(r.id), r.sc, reasons[r.id]))
	}
	return "scores{" + strings.Join(parts, "; ") + "}"
}

// EnvelopeCostFromYAML extracts a 0..1 intensity hint from envelope YAML (best-effort).
// Uses sustain level when present; eco≈0.35, balanced≈0.55, high≈0.85 defaults by name/performance.
func EnvelopeCostFromYAML(yamlBytes []byte) float64 {
	if len(yamlBytes) == 0 {
		return 0
	}
	s := strings.ToLower(string(yamlBytes))
	// Prefer explicit sustain level if parseable cheaply.
	if i := strings.Index(s, "sustain:"); i >= 0 {
		chunk := s[i:]
		if j := strings.Index(chunk, "level:"); j >= 0 && j < 80 {
			rest := strings.TrimSpace(chunk[j+6:])
			var v float64
			if _, err := fmt.Sscanf(rest, "%f", &v); err == nil {
				if v < 0 {
					v = 0
				}
				if v > 1 {
					v = 1
				}
				return v
			}
		}
	}
	switch {
	case strings.Contains(s, "performance: eco") || strings.Contains(s, "name: eco"):
		return 0.35
	case strings.Contains(s, "performance: high") || strings.Contains(s, "name: high"):
		return 0.85
	case strings.Contains(s, "performance: balanced"):
		return 0.55
	default:
		return 0.5
	}
}
