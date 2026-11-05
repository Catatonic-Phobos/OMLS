package main

import "testing"

func TestRenderReadinessDistinguishesBackendFromApplication(t *testing.T) {
	p := renderProbe{Backend: "gfxstream", Initialized: true, Device: "NVIDIA GeForce GTX 1050", API: 1}
	if e := validateRenderProbe(p, "GTX 1050"); e != nil {
		t.Fatal(e)
	}
	if p.ICDReady || p.PresentationReady {
		t.Fatal("hardware discovery implied a working application path")
	}
	if e := validateRenderProbe(p, "910M"); e == nil {
		t.Fatal("accepted wrong GPU")
	}
	p.Initialized = false
	if e := validateRenderProbe(p, ""); e == nil {
		t.Fatal("uninitialized backend accepted")
	}
}

func TestSessionRequiresMultiplexedProtocolAgreement(t *testing.T) {
	p := renderProbe{Backend: "gfxstream", Initialized: true, Device: "NVIDIA GeForce GTX 1050", API: 1}
	if e := validateRenderSession(p, ""); e == nil {
		t.Fatal("single-channel host accepted as a multiplexed session")
	}
	p.Multiplexed = true
	p.ProtocolVersion = 2
	if e := validateRenderSession(p, ""); e == nil {
		t.Fatal("unsupported session version accepted")
	}
	p.ProtocolVersion = 1
	if e := validateRenderSession(p, "GTX 1050"); e != nil {
		t.Fatal(e)
	}
}
