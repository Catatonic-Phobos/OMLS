package borrow

import "testing"

func TestDecideTextureGoesToFreestRAM(t *testing.T) {
	self := Offer{Name: "POCKET-STUDIO", CPU: 91, CPUKnown: true, RAM: 40, RAMKnown: true}
	peers := []Offer{
		{Name: "midi-hub", Addr: "192.0.2.8:7444", RAM: 70, RAMKnown: true, RAMFree: 2 << 30},
		{Name: "STUDIO", Addr: "192.0.2.10:7444", RAM: 20, RAMKnown: true, RAMFree: 8 << 30},
		{Name: "POCKET-STUDIO", Addr: "192.0.2.11:7444", RAM: 10, RAMKnown: true, RAMFree: 20 << 30},
	}
	d := Decide(self, peers)
	if d.Texture != "→ STUDIO" || d.Shader != "idle" {
		t.Fatalf("decision texture %q shader %q", d.Texture, d.Shader)
	}
	if len(d.Placements) != 1 || d.Placements[0].Function != FunctionTexture || d.Placements[0].Peer != "STUDIO" {
		t.Fatalf("placements %+v", d.Placements)
	}
}

func TestDecideShaderGoesToFreestVRAM(t *testing.T) {
	self := Offer{Name: "POCKET-STUDIO", GPU: 88, GPUKnown: true}
	peers := []Offer{
		{Name: "midi-hub", Addr: "192.0.2.8:7444", VRAMKnown: true, VRAMFree: 6 << 30, GPU: 10, GPUKnown: true, RAM: 10, RAMKnown: true, RAMFree: 1},
		{Name: "STUDIO", Addr: "192.0.2.10:7444", VRAMKnown: true, VRAMFree: 1 << 30, GPU: 10, GPUKnown: true},
		{Name: "busy-gpu", Addr: "192.0.2.12:7444", VRAMKnown: true, VRAMFree: 20 << 30, GPU: 95, GPUKnown: true},
	}
	d := Decide(self, peers)
	if d.Shader != "→ midi-hub" || d.Texture != "idle" {
		t.Fatalf("decision %+v", d)
	}
}

func TestDecideTieBreaksByHostname(t *testing.T) {
	self := Offer{Name: "local", RAM: 90, RAMKnown: true}
	peers := []Offer{
		{Name: "b", Addr: "b:7444", RAM: 10, RAMKnown: true, RAMFree: 100},
		{Name: "a", Addr: "a:7444", RAM: 10, RAMKnown: true, RAMFree: 100},
	}
	d := Decide(self, peers)
	if d.Texture != "→ a" {
		t.Fatalf("texture %q", d.Texture)
	}
}

func TestDecideSaturatedWhenEveryPeerIsHot(t *testing.T) {
	self := Offer{Name: "local", CPU: 80, CPUKnown: true, GPU: 80, GPUKnown: true}
	peers := []Offer{
		{Name: "a", Addr: "a:7444", RAM: 91, RAMKnown: true, RAMFree: 1 << 30, GPU: 90, GPUKnown: true, VRAMKnown: true, VRAMFree: 1 << 30},
	}
	d := Decide(self, peers)
	if d.Texture != "saturated" || d.Shader != "saturated" || len(d.Placements) != 0 {
		t.Fatalf("decision %+v", d)
	}
}

func TestDecideIdleUnderCeiling(t *testing.T) {
	self := Offer{Name: "local", CPU: 79, CPUKnown: true, RAM: 79, RAMKnown: true, GPU: 79, GPUKnown: true}
	peers := []Offer{{Name: "a", Addr: "a:7444", RAM: 1, RAMKnown: true, RAMFree: 1, VRAMKnown: true, VRAMFree: 1}}
	d := Decide(self, peers)
	if d.Texture != "idle" || d.Shader != "idle" || len(d.Placements) != 0 {
		t.Fatalf("decision %+v", d)
	}
}

func TestFormat(t *testing.T) {
	text := Format(Decision{Texture: "→ STUDIO", Shader: "idle"})
	if text != "borrow: texture → STUDIO   shader idle\n\n" {
		t.Fatalf("format %q", text)
	}
}
