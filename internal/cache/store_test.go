package cache

import "testing"

func TestReserveWriteRead(t *testing.T) {
	s := NewStore()
	st, err := s.Reserve(4096, KindMemory, "unit")
	if err != nil {
		t.Fatal(err)
	}
	if st.Capacity != 4096 || st.Kind != KindMemory || st.Used != 0 {
		t.Fatalf("stat %+v", st)
	}
	payload := []byte("abcdef")
	if err := s.WriteAt(10, payload); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadAt(10, len(payload))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("read %q", got)
	}
	if s.Stat().Used != 16 {
		t.Fatalf("used %d", s.Stat().Used)
	}
	if err := s.WriteAt(4090, []byte("too-long")); err == nil {
		t.Fatal("expected write past the reservation to fail")
	}
	rel := s.Release()
	if rel.Capacity != 0 {
		t.Fatalf("released capacity %d", rel.Capacity)
	}
}

func TestNamedSlotKeepsLegacyReservation(t *testing.T) {
	s := NewStore()
	if _, err := s.Reserve(64, KindMemory, "legacy"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAt(0, []byte("legacy")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveNamed("studio/texture", 32, KindMemory, "borrow:texture"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveNamed("studio/shader", 32, KindGraphics, "borrow:shader"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadAt(0, 6)
	if err != nil || string(got) != "legacy" {
		t.Fatalf("legacy %q %v", got, err)
	}
	if s.Stat().Capacity != 64 {
		t.Fatalf("legacy stat %+v", s.Stat())
	}
	tex := s.StatNamed("studio/texture")
	if tex.Capacity != 32 || tex.Kind != KindMemory {
		t.Fatalf("texture %+v", tex)
	}
	s.ReleaseNamed("studio/texture")
	if s.StatNamed("studio/shader").Capacity != 32 {
		t.Fatal("shader slot was dropped with the texture")
	}
	if s.Stat().Used != 6 {
		t.Fatalf("legacy used %d", s.Stat().Used)
	}
}

func TestReserveRejectsUnknownKind(t *testing.T) {
	s := NewStore()
	if _, err := s.Reserve(32, "disk", ""); err == nil {
		t.Fatal("expected unknown kind to fail")
	}
}
