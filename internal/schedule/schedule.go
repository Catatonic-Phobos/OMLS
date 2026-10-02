// Package schedule scores Resource Graph nodes for function placement (OMLS 0.3).
package schedule

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

// Request is a minimal function placement request.
type Request struct {
	Function string
	Requires []string
	Workers  int
}

// Candidate is a scored available node.
type Candidate struct {
	NodeID   string
	Hostname string
	Virt     string
	Capacity int // max workers (logical CPUs)
	Score    float64
	Reason   string
}

// Allocation maps node ID → worker count.
type Allocation map[string]int

// Options tunes scheduler v0.
type Options struct {
	LocalHostname  string  // master's hostname; matching agents get a locality bonus
	ThermalCeiling float64 // Celsius; 0 → 85
	WSLPenalty     float64 // subtracted from score; default 2
}

// Plan picks an initial worker split across capable available nodes.
func Plan(nodes []graph.NodeEntry, req Request, opt Options) (Allocation, []Candidate, error) {
	if req.Workers < 1 {
		return nil, nil, fmt.Errorf("workers must be >= 1")
	}
	if req.Function == "" {
		req.Function = "parallel_workers"
	}
	if len(req.Requires) == 0 {
		req.Requires = []string{"compute", "parallelizable"}
	}
	if opt.ThermalCeiling <= 0 {
		opt.ThermalCeiling = 85
	}
	if opt.WSLPenalty == 0 {
		opt.WSLPenalty = 2
	}

	cands := []Candidate{}
	for _, n := range nodes {
		if n.Status != graph.StatusAvailable || n.Profile == nil {
			continue
		}
		cap, ok := capacity(n.Profile, req.Requires)
		if !ok || cap < 1 {
			continue
		}
		score, reason := scoreNode(n, cap, opt)
		cands = append(cands, Candidate{
			NodeID:   n.ID,
			Hostname: n.Hostname,
			Virt:     n.Virt,
			Capacity: cap,
			Score:    score,
			Reason:   reason,
		})
	}
	if len(cands) == 0 {
		return nil, nil, fmt.Errorf("no available nodes with capabilities %v", req.Requires)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Score == cands[j].Score {
			return cands[i].NodeID < cands[j].NodeID
		}
		return cands[i].Score > cands[j].Score
	})

	alloc := splitWorkers(cands, req.Workers)
	return alloc, cands, nil
}

func capacity(doc *rdl.Document, requires []string) (int, bool) {
	best := 0
	matched := false
	for _, r := range doc.Resources {
		if !hasCaps(r.Capabilities, requires) {
			continue
		}
		matched = true
		n := 1
		if r.Class == "compute" {
			if v, ok := intAttr(r.Attrs, "logical_cpus"); ok && v > 0 {
				n = v
			} else if v, ok := intAttr(r.Attrs, "online_cpus"); ok && v > 0 {
				n = v
			} else if v, ok := intAttr(r.Attrs, "cores"); ok && v > 0 {
				n = v
			}
		}
		if n > best {
			best = n
		}
	}
	return best, matched
}

func hasCaps(have, need []string) bool {
	set := map[string]struct{}{}
	for _, c := range have {
		set[strings.ToLower(c)] = struct{}{}
	}
	for _, n := range need {
		if _, ok := set[strings.ToLower(n)]; !ok {
			return false
		}
	}
	return true
}

func intAttr(attrs map[string]any, key string) (int, bool) {
	if attrs == nil {
		return 0, false
	}
	v, ok := attrs[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func scoreNode(n graph.NodeEntry, capacity int, opt Options) (float64, string) {
	parts := []string{fmt.Sprintf("capacity=%d", capacity)}
	score := float64(capacity) * 10

	if n.Hostname != "" && opt.LocalHostname != "" && n.Hostname == opt.LocalHostname {
		score += 5
		parts = append(parts, "locality=local+5")
	} else {
		score -= 1
		parts = append(parts, "locality=remote-1")
	}

	virt := strings.ToLower(n.Virt)
	if virt == "wsl" {
		score -= opt.WSLPenalty
		parts = append(parts, fmt.Sprintf("virt=wsl-%.0f", opt.WSLPenalty))
	} else if virt != "" {
		parts = append(parts, "virt="+virt)
	}

	if n.Telemetry != nil {
		load := n.Telemetry.CPULoad
		score -= load * 2
		parts = append(parts, fmt.Sprintf("load=%.2f", load))
		if n.Telemetry.TemperatureKnown {
			temp := n.Telemetry.TemperatureC
			parts = append(parts, fmt.Sprintf("temp=%.1f", temp))
			if temp >= opt.ThermalCeiling {
				score -= 50
				parts = append(parts, "thermal=over_ceiling-50")
			} else if temp > opt.ThermalCeiling-10 {
				score -= (temp - (opt.ThermalCeiling - 10)) * 2
				parts = append(parts, "thermal=warm")
			}
		} else {
			parts = append(parts, "thermal=unknown")
		}
	} else {
		parts = append(parts, "telemetry=none")
	}

	if score < 0.1 {
		score = 0.1
	}
	return score, strings.Join(parts, " ")
}

// splitWorkers distributes workers proportionally to score, clamped by capacity.
func splitWorkers(cands []Candidate, workers int) Allocation {
	alloc := Allocation{}
	if len(cands) == 0 {
		return alloc
	}
	totalScore := 0.0
	totalCap := 0
	for _, c := range cands {
		totalScore += c.Score
		totalCap += c.Capacity
		alloc[c.NodeID] = 0
	}
	if totalCap == 0 {
		return alloc
	}
	if workers > totalCap {
		workers = totalCap
	}

	// Largest remainder method on score shares, then clamp to capacity.
	type frac struct {
		id    string
		floor int
		rem   float64
		cap   int
	}
	rows := make([]frac, 0, len(cands))
	assigned := 0
	for _, c := range cands {
		share := (c.Score / totalScore) * float64(workers)
		f := int(math.Floor(share))
		if f > c.Capacity {
			f = c.Capacity
		}
		rows = append(rows, frac{id: c.NodeID, floor: f, rem: share - math.Floor(share), cap: c.Capacity})
		assigned += f
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].rem == rows[j].rem {
			return rows[i].id < rows[j].id
		}
		return rows[i].rem > rows[j].rem
	})
	for assigned < workers {
		progress := false
		for i := range rows {
			if assigned >= workers {
				break
			}
			if rows[i].floor < rows[i].cap {
				rows[i].floor++
				assigned++
				progress = true
			}
		}
		if !progress {
			break
		}
	}
	for _, r := range rows {
		alloc[r.id] = r.floor
	}
	return alloc
}
