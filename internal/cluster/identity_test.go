package cluster

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityPersistsAcrossLoad(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadOrCreate(dir, "from-machine-id")
	if err != nil {
		t.Fatal(err)
	}
	if first.NodeID != "from-machine-id" {
		t.Fatalf("node_id=%q", first.NodeID)
	}
	second, err := LoadOrCreate(dir, "different-machine-id")
	if err != nil {
		t.Fatal(err)
	}
	if second.NodeID != "from-machine-id" {
		t.Fatalf("reboot changed node_id to %q", second.NodeID)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "identity.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("identity file empty")
	}
}

func TestIdentityGeneratesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	id, err := LoadOrCreate(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(id.NodeID) < 8 {
		t.Fatalf("generated id %q", id.NodeID)
	}
	again, err := LoadOrCreate(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if again.NodeID != id.NodeID {
		t.Fatalf("generated id changed %q -> %q", id.NodeID, again.NodeID)
	}
}
