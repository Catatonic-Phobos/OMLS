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

func TestReserveRejectsUnknownKind(t *testing.T) {
	s := NewStore()
	if _, err := s.Reserve(32, "disk", ""); err == nil {
		t.Fatal("expected unknown kind to fail")
	}
}
