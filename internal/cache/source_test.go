package cache

import (
	"os"
	"testing"
)

func TestCopyResident(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "page")
	if err != nil {
		t.Fatal(err)
	}
	page := os.Getpagesize()
	buf := make([]byte, page*2)
	for i := range buf {
		buf[i] = byte(i)
	}
	if _, err := f.Write(buf); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	// Fault the file into the page cache, then copy only resident bytes.
	raw, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != len(buf) {
		t.Fatalf("read %d", len(raw))
	}
	got, err := CopyResident(f.Name(), page)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != page {
		t.Fatalf("resident %d want %d", len(got), page)
	}
	if got[0] != 0 || got[1] != 1 {
		t.Fatalf("bytes %v", got[:4])
	}
}
