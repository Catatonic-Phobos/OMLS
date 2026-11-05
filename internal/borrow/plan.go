// Package borrow places a texture or shader slice on a peer when this
// machine crosses the ceiling. The decision uses only published offers,
// so every node computes the same assignment.
package borrow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/cache"
)

const (
	// Ceiling is the used-percent at which a function leaves this machine.
	Ceiling = 80.0
	// Release is the used-percent below which a held slice comes back.
	Release = 70.0

	FunctionTexture = "texture"
	FunctionShader  = "shader"

	// ReserveBytes is the RAM a peer holds for one borrowed function.
	ReserveBytes = 32 << 20
	// FillBytes is the prefix copied and read back to prove the slice.
	FillBytes = 1 << 20
)

// Offer is one machine's published headroom plus the cache address to dial.
type Offer struct {
	Name      string
	Addr      string
	CPU       float64
	CPUKnown  bool
	RAM       float64
	RAMKnown  bool
	RAMFree   int64
	GPU       float64
	GPUKnown  bool
	VRAMFree  int64
	VRAMKnown bool
}

// Placement is one function moved to a peer.
type Placement struct {
	Function string
	Kind     string
	Peer     string
	Addr     string
}

// Decision is the assignment every node can recompute.
// Texture and Shader are "idle", "saturated", or "→ <peer>".
type Decision struct {
	Texture    string
	Shader     string
	Placements []Placement
}

// FromHeadroom copies a cache offer and attaches the dial address.
func FromHeadroom(h cache.Headroom, addr string) Offer {
	return Offer{
		Name:      h.Hostname,
		Addr:      addr,
		CPU:       h.CPU,
		CPUKnown:  h.CPUKnown,
		RAM:       h.RAM,
		RAMKnown:  h.RAMKnown,
		RAMFree:   h.RAMFree,
		GPU:       h.GPU,
		GPUKnown:  h.GPUKnown,
		VRAMFree:  h.VRAMFree,
		VRAMKnown: h.VRAMKnown,
	}
}

// Decide assigns texture to the peer with the most free RAM and shader to
// the peer with the most free VRAM. A peer at or above the ceiling for that
// resource is skipped. The tie break is the hostname.
func Decide(self Offer, peers []Offer) Decision {
	d := Decision{Texture: "idle", Shader: "idle"}
	open := filter(self, peers)
	if hotCPU(self) || hotRAM(self) {
		if p, ok := pick(open, func(o Offer) (int64, bool) {
			if !o.RAMKnown || o.RAM >= Ceiling || o.Addr == "" {
				return 0, false
			}
			return o.RAMFree, true
		}); ok {
			d.Texture = "→ " + p.Name
			d.Placements = append(d.Placements, Placement{
				Function: FunctionTexture, Kind: cache.KindMemory, Peer: p.Name, Addr: p.Addr,
			})
		} else {
			d.Texture = "saturated"
		}
	}
	if hotGPU(self) {
		if p, ok := pick(open, func(o Offer) (int64, bool) {
			if !o.VRAMKnown || o.Addr == "" || (o.GPUKnown && o.GPU >= Ceiling) {
				return 0, false
			}
			return o.VRAMFree, true
		}); ok {
			d.Shader = "→ " + p.Name
			d.Placements = append(d.Placements, Placement{
				Function: FunctionShader, Kind: cache.KindGraphics, Peer: p.Name, Addr: p.Addr,
			})
		} else {
			d.Shader = "saturated"
		}
	}
	return d
}

// Format renders the borrow line for the status screen.
func Format(d Decision) string {
	return fmt.Sprintf("borrow: texture %s   shader %s\n\n", d.Texture, d.Shader)
}

// StillHot reports whether a function already placed should stay until the
// machine drops under the release line.
func StillHot(self Offer, function string) bool {
	switch function {
	case FunctionTexture:
		return (self.CPUKnown && self.CPU >= Release) || (self.RAMKnown && self.RAM >= Release)
	case FunctionShader:
		return self.GPUKnown && self.GPU >= Release
	default:
		return false
	}
}

func hotCPU(o Offer) bool { return o.CPUKnown && o.CPU >= Ceiling }
func hotRAM(o Offer) bool { return o.RAMKnown && o.RAM >= Ceiling }
func hotGPU(o Offer) bool { return o.GPUKnown && o.GPU >= Ceiling }

func filter(self Offer, peers []Offer) []Offer {
	var out []Offer
	for _, p := range peers {
		if p.Name == "" || strings.EqualFold(p.Name, self.Name) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func pick(peers []Offer, score func(Offer) (int64, bool)) (Offer, bool) {
	type row struct {
		o     Offer
		score int64
	}
	var rows []row
	for _, p := range peers {
		s, ok := score(p)
		if !ok {
			continue
		}
		rows = append(rows, row{o: p, score: s})
	}
	if len(rows) == 0 {
		return Offer{}, false
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		return rows[i].o.Name < rows[j].o.Name
	})
	return rows[0].o, true
}
