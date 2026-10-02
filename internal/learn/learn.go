// Package learn is the OMLS 0.3 Learning Plane v0 (EWMA, no ML libs).
package learn

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/schedule"
)

// Stats is per-node EWMA state.
type Stats struct {
	DurationEWMA  float64 // milliseconds
	TempDeltaEWMA float64
	Samples       int
}

// Plane holds explainable statistical learning state.
type Plane struct {
	Alpha          float64 // EWMA smoothing, default 0.3
	ThermalCeiling float64 // Celsius
	nodes          map[string]*Stats
}

// New creates a learning plane.
func New(alpha, thermalCeiling float64) *Plane {
	if alpha <= 0 || alpha > 1 {
		alpha = 0.3
	}
	if thermalCeiling <= 0 {
		thermalCeiling = 85
	}
	return &Plane{
		Alpha:          alpha,
		ThermalCeiling: thermalCeiling,
		nodes:          map[string]*Stats{},
	}
}

// Observe updates EWMAs for a node after a work unit / round.
func (p *Plane) Observe(nodeID string, durationMs float64, tempDelta float64, tempKnown bool) {
	st := p.nodes[nodeID]
	if st == nil {
		st = &Stats{}
		p.nodes[nodeID] = st
	}
	if st.Samples == 0 {
		st.DurationEWMA = durationMs
		if tempKnown {
			st.TempDeltaEWMA = tempDelta
		}
	} else {
		st.DurationEWMA = p.Alpha*durationMs + (1-p.Alpha)*st.DurationEWMA
		if tempKnown {
			st.TempDeltaEWMA = p.Alpha*tempDelta + (1-p.Alpha)*st.TempDeltaEWMA
		}
	}
	st.Samples++
}

// Snapshot returns a copy of current EWMAs.
func (p *Plane) Snapshot() map[string]Stats {
	out := make(map[string]Stats, len(p.nodes))
	for k, v := range p.nodes {
		out[k] = *v
	}
	return out
}

// DurationEWMA returns the EWMA or 0.
func (p *Plane) DurationEWMA(nodeID string) float64 {
	if st := p.nodes[nodeID]; st != nil {
		return st.DurationEWMA
	}
	return 0
}

// Seed installs prior stats (e.g. from persisted Behavior Profiles) before a demo.
func (p *Plane) Seed(nodeID string, durationEWMA, tempDeltaEWMA float64, samples int) {
	if samples <= 0 {
		return
	}
	p.nodes[nodeID] = &Stats{
		DurationEWMA:  durationEWMA,
		TempDeltaEWMA: tempDeltaEWMA,
		Samples:       samples,
	}
}

// Adjust shifts workers toward faster / cooler nodes. Returns new allocation and reasons.
func (p *Plane) Adjust(prev schedule.Allocation, capacity map[string]int, tempNow map[string]float64, tempKnown map[string]bool) (schedule.Allocation, string) {
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
		return next, "no rebalance: fewer than 2 nodes with work"
	}

	reasons := []string{}

	// 1) Thermal escape: move one worker off any node over ceiling.
	for _, id := range ids {
		if !tempKnown[id] {
			continue
		}
		if tempNow[id] < p.ThermalCeiling {
			continue
		}
		donor := id
		if next[donor] <= 0 {
			continue
		}
		recv := bestReceiver(ids, next, capacity, p, donor, true)
		if recv == "" {
			reasons = append(reasons, fmt.Sprintf("thermal: %s over ceiling but no receiver capacity", short(donor)))
			continue
		}
		next[donor]--
		next[recv]++
		reasons = append(reasons, fmt.Sprintf("thermal: move 1 %s→%s (temp %.1f≥%.0f)", short(donor), short(recv), tempNow[donor], p.ThermalCeiling))
	}

	// 2) Duration: if slowest EWMA > 1.25× fastest, move 1 worker toward faster.
	fastID, slowID := "", ""
	fast, slow := math.MaxFloat64, 0.0
	for _, id := range ids {
		if next[id] == 0 && prev[id] == 0 {
			continue
		}
		d := p.DurationEWMA(id)
		if d <= 0 {
			continue
		}
		if d < fast {
			fast = d
			fastID = id
		}
		if d > slow {
			slow = d
			slowID = id
		}
	}
	if fastID != "" && slowID != "" && fastID != slowID && slow > fast*1.25 {
		if next[slowID] > 0 && next[fastID] < capacity[fastID] {
			next[slowID]--
			next[fastID]++
			reasons = append(reasons, fmt.Sprintf("duration: move 1 %s→%s (ewma %.0fms vs %.0fms)", short(slowID), short(fastID), slow, fast))
		} else {
			reasons = append(reasons, fmt.Sprintf("duration: would prefer %s over %s but capacity/clamp blocks", short(fastID), short(slowID)))
		}
	}

	if len(reasons) == 0 {
		return next, "hold allocation: no thermal or duration signal strong enough"
	}
	return next, strings.Join(reasons, "; ")
}

func bestReceiver(ids []string, alloc schedule.Allocation, capacity map[string]int, p *Plane, donor string, preferCool bool) string {
	best := ""
	bestScore := -1e9
	for _, id := range ids {
		if id == donor {
			continue
		}
		if alloc[id] >= capacity[id] {
			continue
		}
		score := 100.0
		if d := p.DurationEWMA(id); d > 0 {
			score -= d / 100
		}
		if preferCool {
			if st := p.nodes[id]; st != nil {
				score -= st.TempDeltaEWMA
			}
		}
		if score > bestScore {
			bestScore = score
			best = id
		}
	}
	return best
}

func short(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
