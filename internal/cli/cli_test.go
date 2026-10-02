// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func TestUsageAndVersion(t *testing.T) {
	var out, err bytes.Buffer
	if code := Run(nil, &out, &err); code != 0 || !strings.Contains(out.String(), "omls agent discover") {
		t.Fatalf("code %d out %q err %q", code, out.String(), err.String())
	}
	out.Reset()
	err.Reset()
	if code := Run([]string{"version"}, &out, &err); code != 0 || strings.TrimSpace(out.String()) != "omls 0.1.0" {
		t.Fatalf("code %d out %q", code, out.String())
	}
	out.Reset()
	err.Reset()
	if code := Run([]string{"nope"}, &out, &err); code != 2 || !strings.Contains(err.String(), "unknown command") {
		t.Fatalf("code %d err %q", code, err.String())
	}
	out.Reset()
	err.Reset()
	if code := Run([]string{"agent"}, &out, &err); code != 2 {
		t.Fatalf("code %d", code)
	}
	out.Reset()
	err.Reset()
	if code := Run([]string{"agent", "discover", "--help"}, &out, &err); code != 0 || !strings.Contains(out.String(), "--root") {
		t.Fatalf("code %d out %q err %q", code, out.String(), err.String())
	}
}

func TestDiscoverFixtureCommand(t *testing.T) {
	root := fixtureRoot(t)
	var out, err bytes.Buffer
	code := Run([]string{"agent", "discover", "--root", root}, &out, &err)
	if code != 0 {
		t.Fatalf("code %d err %s", code, err.String())
	}
	doc, decErr := rdl.Decode(&out)
	if decErr != nil {
		t.Fatal(decErr)
	}
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	if doc.RDL != "0.1" || doc.Kind != rdl.KindNodeInventory {
		t.Fatalf("%s %s", doc.RDL, doc.Kind)
	}
	if doc.Node.Hostname != "omls-node" || doc.Node.MachineID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("%+v", doc.Node)
	}
	if doc.Node.CPU.Logical != 2 || doc.Node.Memory.TotalBytes != 1073741824 {
		t.Fatalf("cpu %+v mem %+v", doc.Node.CPU, doc.Node.Memory)
	}
	if len(doc.Node.Accelerators) != 2 || doc.Node.Accelerators[1].Vendor != "nvidia" {
		t.Fatalf("%+v", doc.Node.Accelerators)
	}
	if len(doc.Node.BlockDevices) != 1 || doc.Node.BlockDevices[0].Name != "vda" {
		t.Fatalf("%+v", doc.Node.BlockDevices)
	}
	foundSpace := false
	for _, mnt := range doc.Node.Mounts {
		if mnt.Target == "/mnt/model cache" {
			foundSpace = true
		}
	}
	if !foundSpace {
		t.Fatalf("%+v", doc.Node.Mounts)
	}
	if doc.Node.OS.ID != "ubuntu" || doc.Node.OS.Kernel == "" || doc.Node.OS.Arch == "" {
		t.Fatalf("%+v", doc.Node.OS)
	}

	out.Reset()
	err.Reset()
	path := filepath.Join(t.TempDir(), "node.json")
	code = Run([]string{"agent", "discover", "--root", root, "--compact", "--output", path}, &out, &err)
	if code != 0 {
		t.Fatalf("code %d err %s", code, err.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout %q", out.String())
	}
	body, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if bytes.Count(body, []byte("\n")) != 1 {
		t.Fatalf("compact file:\n%s", body)
	}
	if _, decErr = rdl.Decode(bytes.NewReader(body)); decErr != nil {
		t.Fatal(decErr)
	}
}

func TestDiscoverCommandErrors(t *testing.T) {
	var out, err bytes.Buffer
	if code := Run([]string{"agent", "discover", "extra"}, &out, &err); code != 2 {
		t.Fatalf("code %d", code)
	}
	out.Reset()
	err.Reset()
	missing := filepath.Join(t.TempDir(), "missing")
	if code := Run([]string{"agent", "discover", "--root", missing}, &out, &err); code != 1 || !strings.Contains(err.String(), "omls:") {
		t.Fatalf("code %d err %q", code, err.String())
	}
	out.Reset()
	err.Reset()
	if code := Run([]string{"agent", "discover", "--nope"}, &out, &err); code != 2 {
		t.Fatalf("code %d err %q", code, err.String())
	}
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "discover", "testdata", "root")
}
