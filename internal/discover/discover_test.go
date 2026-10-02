package discover_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Catatonic-Phobos/OMLS/internal/discover"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func TestDiscoverSmokeHost(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("discover is Linux-only")
	}
	res, err := discover.Discover(discover.Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if err := res.Document.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Document.Node.ID == "" {
		t.Fatal("empty node id")
	}
	if res.Document.Node.OS.Family != "linux" {
		t.Fatalf("os.family=%q", res.Document.Node.OS.Family)
	}
	if res.Document.Node.Virt == "" {
		t.Fatal("virt tag missing")
	}

	hasCPU := false
	hasMem := false
	for _, r := range res.Document.Resources {
		switch r.Class {
		case "compute":
			hasCPU = true
		case "memory":
			hasMem = true
		}
	}
	if !hasCPU {
		t.Fatal("expected cpu resource")
	}
	if !hasMem {
		t.Fatal("expected memory resource")
	}

	// Round-trip YAML
	raw, err := res.Document.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	if _, err := rdl.ParseYAML(raw); err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}

	t.Logf("node_id=%s virt=%s resources=%d transports=%d warnings=%d",
		res.Document.Node.ID, res.Document.Node.Virt,
		len(res.Document.Resources), len(res.Document.Transports), len(res.Warnings))
}

func TestDiscoverFixtureDegrades(t *testing.T) {
	dir := t.TempDir()
	// Minimal fixture: only machine-id + meminfo + cpuinfo + empty net
	mustWrite(t, filepath.Join(dir, "etc", "machine-id"), "deadbeefcafebabe0123456789abcdef\n")
	mustWrite(t, filepath.Join(dir, "etc", "os-release"), "PRETTY_NAME=\"Fixture OS\"\n")
	mustWrite(t, filepath.Join(dir, "proc", "sys", "kernel", "osrelease"), "6.1.0-fixture\n")
	mustWrite(t, filepath.Join(dir, "proc", "cpuinfo"), "processor\t: 0\nmodel name\t: Fixture CPU\ncpu cores\t: 1\n")
	mustWrite(t, filepath.Join(dir, "proc", "meminfo"), "MemTotal:       1024000 kB\nMemAvailable:    512000 kB\n")
	mustMkdir(t, filepath.Join(dir, "sys", "class", "net"))
	mustMkdir(t, filepath.Join(dir, "sys", "block"))
	mustMkdir(t, filepath.Join(dir, "sys", "bus", "pci", "devices"))
	mustMkdir(t, filepath.Join(dir, "sys", "bus", "usb", "devices"))
	mustMkdir(t, filepath.Join(dir, "sys", "devices", "system", "cpu"))

	res, err := discover.Discover(discover.Options{SysRoot: dir})
	if err != nil {
		t.Fatalf("Discover fixture: %v", err)
	}
	if res.Document.Node.ID != "deadbeefcafebabe0123456789abcdef" {
		t.Fatalf("node id=%q", res.Document.Node.ID)
	}
	if res.Document.Node.OS.Pretty != "Fixture OS" {
		t.Fatalf("pretty=%q", res.Document.Node.OS.Pretty)
	}
	// Missing PCI/USB/thermal must warn, not fail.
	if len(res.Warnings) == 0 {
		t.Fatal("expected degradation warnings")
	}
}

func TestDetectWSLVirt(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "etc", "machine-id"), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	mustWrite(t, filepath.Join(dir, "etc", "os-release"), "PRETTY_NAME=\"Ubuntu WSL\"\n")
	mustWrite(t, filepath.Join(dir, "proc", "sys", "kernel", "osrelease"), "5.15.0-microsoft-standard-WSL2\n")
	mustWrite(t, filepath.Join(dir, "proc", "version"), "Linux version 5.15.0-microsoft-standard-WSL2\n")
	mustWrite(t, filepath.Join(dir, "proc", "cpuinfo"), "processor\t: 0\nmodel name\t: WSL CPU\n")
	mustWrite(t, filepath.Join(dir, "proc", "meminfo"), "MemTotal:       2048000 kB\nMemAvailable:   1024000 kB\n")
	mustMkdir(t, filepath.Join(dir, "proc", "sys", "fs", "binfmt_misc"))
	mustWrite(t, filepath.Join(dir, "proc", "sys", "fs", "binfmt_misc", "WSLInterop"), "enabled\n")
	mustMkdir(t, filepath.Join(dir, "sys", "class", "net"))
	mustMkdir(t, filepath.Join(dir, "sys", "block"))
	mustMkdir(t, filepath.Join(dir, "sys", "bus", "pci", "devices"))
	mustMkdir(t, filepath.Join(dir, "sys", "bus", "usb", "devices"))
	mustMkdir(t, filepath.Join(dir, "sys", "devices", "system", "cpu"))

	res, err := discover.Discover(discover.Options{SysRoot: dir})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if res.Document.Node.Virt != "wsl" {
		t.Fatalf("virt=%q want wsl", res.Document.Node.Virt)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
