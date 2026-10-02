// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package discover

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func TestDiscoverFixture(t *testing.T) {
	doc, err := Discover(Options{
		Root: fixtureRoot(t),
		Now: func() time.Time {
			// 01:28:00.9 at UTC-3 is 04:28:00.9Z, which truncates to 04:28:00Z.
			return time.Date(2026, 10, 2, 1, 28, 0, 900_000_000, time.FixedZone("local", -3*3600))
		},
		Uname: func() (Uname, error) {
			return Uname{Sysname: "Linux", Release: "6.8.0-test", Machine: "x86_64"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := rdl.Marshal(doc, false)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(filepath.Dir(fixtureRoot(t)), "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("RDL mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestDiscoverLive(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux userspace")
	}
	if _, err := os.Stat("/proc/cpuinfo"); err != nil {
		t.Skip(err)
	}
	doc, err := Discover(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	if doc.Node.CPU.Logical != runtime.NumCPU() {
		t.Fatalf("logical %d, NumCPU %d", doc.Node.CPU.Logical, runtime.NumCPU())
	}
	if doc.Node.Hostname == "" || doc.Node.OS.Kernel == "" || doc.Node.OS.Arch == "" {
		t.Fatalf("%+v", doc.Node)
	}
	if _, err := os.Stat("/sys/block/vda"); err == nil {
		found := false
		for _, disk := range doc.Node.BlockDevices {
			if disk.Name == "vda" {
				found = true
			}
			if skipBlock(disk.Name) {
				t.Fatalf("included skipped disk %s", disk.Name)
			}
		}
		if !found {
			t.Fatal("missing vda")
		}
	}
}

func TestDiscoverErrors(t *testing.T) {
	_, err := Discover(Options{
		Uname: func() (Uname, error) {
			return Uname{Sysname: "FreeBSD", Release: "14", Machine: "amd64"}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported kernel") {
		t.Fatalf("got %v", err)
	}

	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"proc/sys/kernel/hostname": "minimal\n",
		"proc/meminfo":             "MemTotal: 1024 kB\n",
	})
	_, err = Discover(Options{Root: root, Uname: linuxUname})
	if err == nil || !strings.Contains(err.Error(), "cpuinfo") {
		t.Fatalf("got %v", err)
	}

	writeTree(t, root, map[string]string{
		"proc/cpuinfo": "processor : 0\n",
	})
	os.Remove(filepath.Join(root, "proc/meminfo"))
	_, err = Discover(Options{Root: root, Uname: linuxUname})
	if err == nil || !strings.Contains(err.Error(), "meminfo") {
		t.Fatalf("got %v", err)
	}
}

func TestMachineIDFallback(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"proc/sys/kernel/hostname": "minimal\n",
		"proc/cpuinfo":             "processor : 0\n",
		"proc/meminfo":             "MemTotal: 1024 kB\n",
		"var/lib/dbus/machine-id":  "abcdef0123456789abcdef0123456789\n",
	})
	doc, err := Discover(Options{Root: root, Uname: linuxUname})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Node.MachineID != "abcdef0123456789abcdef0123456789" {
		t.Fatalf("machine id %q", doc.Node.MachineID)
	}
	if doc.Node.OS.Kernel != "6.8.0-test" || doc.Node.CPU.Architecture != "x86_64" {
		t.Fatalf("%+v", doc.Node.OS)
	}
}

func linuxUname() (Uname, error) {
	return Uname{Sysname: "Linux", Release: "6.8.0-test", Machine: "x86_64"}, nil
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	return filepath.Join(filepath.Dir(file), "testdata", "root")
}
