package schedule

import (
	"fmt"
	"sort"

	"github.com/Catatonic-Phobos/OMLS/internal/graph"
)

// FuncRequest is one named function to place on a capable node.
type FuncRequest struct {
	Function string
	Requires []string
	Workers  int
}

// Placement is one exclusive function → node assignment.
type Placement struct {
	Function string
	NodeID   string
	Hostname string
	Virt     string
	Workers  int
	Score    float64
	Reason   string
}

// PlaceExclusive assigns each function to a different node when possible.
// A node is used at most once across the request set so work is actually
// separated across computers instead of only sharded as anonymous workers.
func PlaceExclusive(nodes []graph.NodeEntry, funcs []FuncRequest, opt Options) ([]Placement, error) {
	if len(funcs) == 0 {
		return nil, fmt.Errorf("at least one function is required")
	}
	if opt.ThermalCeiling <= 0 {
		opt.ThermalCeiling = 85
	}
	if opt.WSLPenalty == 0 {
		opt.WSLPenalty = 2
	}

	used := map[string]bool{}
	out := make([]Placement, 0, len(funcs))
	for _, fn := range funcs {
		if fn.Function == "" {
			return nil, fmt.Errorf("function name is required")
		}
		if fn.Workers < 1 {
			fn.Workers = 1
		}
		if len(fn.Requires) == 0 {
			fn.Requires = []string{"compute", "parallelizable"}
		}
		type scored struct {
			n      graph.NodeEntry
			cap    int
			score  float64
			reason string
		}
		var cands []scored
		for _, n := range nodes {
			if n.Status != graph.StatusAvailable || n.Profile == nil || used[n.ID] {
				continue
			}
			cap, ok := capacity(n.Profile, fn.Requires)
			if !ok || cap < 1 {
				continue
			}
			score, reason := scoreNode(n, cap, opt)
			cands = append(cands, scored{n: n, cap: cap, score: score, reason: reason})
		}
		if len(cands) == 0 {
			// Fall back: allow reuse only when every unused node lacks the capability.
			for _, n := range nodes {
				if n.Status != graph.StatusAvailable || n.Profile == nil {
					continue
				}
				cap, ok := capacity(n.Profile, fn.Requires)
				if !ok || cap < 1 {
					continue
				}
				score, reason := scoreNode(n, cap, opt)
				cands = append(cands, scored{n: n, cap: cap, score: score, reason: reason + " reuse=forced"})
			}
		}
		if len(cands) == 0 {
			return nil, fmt.Errorf("no available node for function %q requiring %v", fn.Function, fn.Requires)
		}
		sort.SliceStable(cands, func(i, j int) bool {
			if cands[i].score == cands[j].score {
				return cands[i].n.ID < cands[j].n.ID
			}
			return cands[i].score > cands[j].score
		})
		best := cands[0]
		workers := fn.Workers
		if workers > best.cap {
			workers = best.cap
		}
		used[best.n.ID] = true
		out = append(out, Placement{
			Function: fn.Function,
			NodeID:   best.n.ID,
			Hostname: best.n.Hostname,
			Virt:     best.n.Virt,
			Workers:  workers,
			Score:    best.score,
			Reason:   best.reason + " placement=exclusive",
		})
	}
	return out, nil
}
