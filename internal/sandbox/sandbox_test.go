package sandbox_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
	"github.com/Catatonic-Phobos/OMLS/internal/sandbox"
)

func TestProbeEmptySysfs(t *testing.T) {
	root := t.TempDir()
	res := sandbox.Probe(sandbox.Options{SysRoot: root})
	if res.Backend != sandbox.BackendNone {
		t.Fatalf("backend=%s", res.Backend)
	}
	if res.VFIOPresent || res.UIOPresent {
		t.Fatal("expected no backends")
	}
	if len(res.Warnings) == 0 {
		t.Fatal("expected warning about missing vfio/uio")
	}
	claim := sandbox.Claim(sandbox.Options{SysRoot: root}, "vfio-foo")
	if claim.OK {
		t.Fatal("claim should fail without devices")
	}
	if !claim.Simulated {
		t.Fatal("expected simulated")
	}
}

func TestProbeVFIOAndUIOFixtures(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "sys/bus/vfio/devices/vfio0"))
	mustMkdir(t, filepath.Join(root, "sys/class/uio/uio0"))
	if err := os.WriteFile(filepath.Join(root, "sys/class/uio/uio0/name"), []byte("demo-uio\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := sandbox.Probe(sandbox.Options{SysRoot: root})
	if res.Backend != sandbox.BackendBoth {
		t.Fatalf("backend=%s", res.Backend)
	}
	if !res.VFIOPresent || !res.UIOPresent {
		t.Fatal("expected both present")
	}
	if len(res.Devices) < 2 {
		t.Fatalf("devices=%v", res.Devices)
	}

	doc := rdl.Empty()
	doc.Node.ID = "n"
	doc.Node.Hostname = "h"
	doc.Node.OS.Family = "linux"
	sandbox.MergeIntoDocument(&doc, res)
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	foundSandbox := false
	for _, r := range doc.Resources {
		if r.Class == "sandbox" {
			foundSandbox = true
			break
		}
	}
	if !foundSandbox {
		t.Fatal("expected sandbox resources in RDL")
	}

	c := sandbox.Claim(sandbox.Options{SysRoot: root}, "uio-uio0")
	if !c.OK || !c.Simulated {
		t.Fatalf("claim=%+v", c)
	}
	if c.Backend != sandbox.BackendUIO {
		t.Fatalf("backend=%s", c.Backend)
	}
}

func TestClaimLogicalSandbox0(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "sys/class/vfio"))
	c := sandbox.Claim(sandbox.Options{SysRoot: root}, "sandbox0")
	if !c.OK {
		t.Fatalf("%+v", c)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
