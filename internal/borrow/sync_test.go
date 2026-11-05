package borrow

import (
	"context"
	"testing"
)

type fakeClient struct {
	placed   []string
	released []string
	fail     bool
}

func (f *fakeClient) Place(_ context.Context, addr, name, kind string) error {
	if f.fail {
		return errFail{}
	}
	f.placed = append(f.placed, addr+" "+name+" "+kind)
	return nil
}

func (f *fakeClient) Release(_ context.Context, addr, name string) error {
	f.released = append(f.released, addr+" "+name)
	return nil
}

type errFail struct{}

func (errFail) Error() string { return "fail" }

func TestSyncPlacesAndHoldsAcrossReleaseLine(t *testing.T) {
	self := Offer{Name: "POCKET-STUDIO", CPU: 90, CPUKnown: true}
	peer := Offer{Name: "STUDIO", Addr: "10:7444", RAM: 10, RAMKnown: true, RAMFree: 100}
	c := &fakeClient{}
	var st State
	lines := st.Sync(context.Background(), self, []Offer{peer}, c)
	if len(lines) != 1 || lines[0] != "borrow texture → STUDIO" {
		t.Fatalf("lines %v", lines)
	}
	if len(c.placed) != 1 || st.Holds[FunctionTexture].Addr != "10:7444" {
		t.Fatalf("placed %v hold %+v", c.placed, st.Holds)
	}
	// Still warm: under the ceiling, above the release line. Keep the slice.
	self.CPU = 75
	lines = st.Sync(context.Background(), self, []Offer{peer}, c)
	if len(lines) != 0 || len(c.released) != 0 {
		t.Fatalf("held lines %v released %v", lines, c.released)
	}
	self.CPU = 10
	self.RAM = 10
	self.RAMKnown = true
	lines = st.Sync(context.Background(), self, []Offer{peer}, c)
	if len(c.released) != 1 || len(st.Holds) != 0 {
		t.Fatalf("release lines %v holds %+v", lines, st.Holds)
	}
}

func TestSyncKeepsSliceWhenPeersAreSaturated(t *testing.T) {
	self := Offer{Name: "local", RAM: 95, RAMKnown: true}
	peer := Offer{Name: "a", Addr: "a:7444", RAM: 10, RAMKnown: true, RAMFree: 50}
	c := &fakeClient{}
	var st State
	_ = st.Sync(context.Background(), self, []Offer{peer}, c)
	peer.RAM = 99
	_ = st.Sync(context.Background(), self, []Offer{peer}, c)
	if len(c.released) != 0 || st.Holds[FunctionTexture].Peer != "a" {
		t.Fatalf("released %v hold %+v", c.released, st.Holds[FunctionTexture])
	}
}
