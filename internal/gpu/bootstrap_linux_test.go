//go:build linux && cgo

package gpu

import (
	"os"
	"testing"
)

func TestEnableRusticlDriversPreservesConfiguredDrivers(t *testing.T) {
	t.Setenv("RUSTICL_ENABLE", "radeonsi:0")
	enableRusticlDrivers([]string{"iris", "radeonsi"})
	if got := os.Getenv("RUSTICL_ENABLE"); got != "radeonsi:0,iris" {
		t.Fatalf("RUSTICL_ENABLE=%q", got)
	}
}

func TestIsGraphicsClass(t *testing.T) {
	if !isGraphicsClass("0x030200\n") {
		t.Fatal("expected PCI display class to be recognized")
	}
	if isGraphicsClass("0x060000") {
		t.Fatal("PCI bridge class must not be treated as a GPU")
	}
}
