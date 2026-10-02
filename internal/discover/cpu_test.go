// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package discover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func TestParseCPUInfo(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		want     rdl.CPU
		wantPhys bool
		wantCore bool
		wantErr  string
	}{
		{
			name: "two cores",
			text: "" +
				"processor\t: 0\n" +
				"vendor_id\t: GenuineIntel\n" +
				"model name\t: Intel(R) Xeon(R) Processor\n" +
				"physical id\t: 0\n" +
				"core id\t: 0\n" +
				"cpu cores\t: 2\n" +
				"flags\t: fpu avx avx2 avx512f\n" +
				"\n" +
				"processor\t: 1\n" +
				"vendor_id\t: GenuineIntel\n" +
				"model name\t: Intel(R) Xeon(R) Processor\n" +
				"physical id\t: 0\n" +
				"core id\t: 1\n" +
				"cpu cores\t: 2\n" +
				"flags\t: fpu avx avx2 avx512f\n",
			want: rdl.CPU{
				Vendor:         "GenuineIntel",
				Model:          "Intel(R) Xeon(R) Processor",
				Logical:        2,
				Cores:          2,
				Sockets:        1,
				ThreadsPerCore: 1,
				Flags:          []string{"fpu", "avx", "avx2", "avx512f"},
			},
			wantPhys: true,
			wantCore: true,
		},
		{
			name: "hyperthread",
			text: "" +
				"processor : 0\nphysical id : 0\ncore id : 0\ncpu cores : 1\nflags : fpu\n\n" +
				"processor : 1\nphysical id : 0\ncore id : 0\ncpu cores : 1\nflags : fpu\n",
			want: rdl.CPU{
				Logical:        2,
				Cores:          1,
				Sockets:        1,
				ThreadsPerCore: 2,
				Flags:          []string{"fpu"},
			},
			wantPhys: true,
			wantCore: true,
		},
		{
			name: "two sockets",
			text: "" +
				"processor : 0\nphysical id : 0\ncore id : 0\n\n" +
				"processor : 1\nphysical id : 0\ncore id : 1\n\n" +
				"processor : 2\nphysical id : 1\ncore id : 0\n\n" +
				"processor : 3\nphysical id : 1\ncore id : 1\n",
			want: rdl.CPU{
				Logical:        4,
				Cores:          4,
				Sockets:        2,
				ThreadsPerCore: 1,
			},
			wantPhys: true,
			wantCore: true,
		},
		{
			name: "arm",
			text: "" +
				"processor : 0\nFeatures : fp asimd aes\nCPU implementer : 0x41\nCPU part : 0xd0c\n\n" +
				"processor : 1\nFeatures : fp asimd aes\nCPU implementer : 0x41\nCPU part : 0xd0c\n",
			want: rdl.CPU{
				Vendor:         "ARM",
				Model:          "CPU part 0xd0c",
				Logical:        2,
				Cores:          2,
				Sockets:        1,
				ThreadsPerCore: 1,
				Flags:          []string{"fp", "asimd", "aes"},
			},
		},
		{
			name:    "empty",
			text:    "model name : orphan\n",
			wantErr: "no processors",
		},
		{
			name:    "bad cpu cores",
			text:    "processor : 0\ncpu cores : nope\n",
			wantErr: "cpu cores",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, phys, core, err := parseCPUInfo(tt.text)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if phys != tt.wantPhys || core != tt.wantCore {
				t.Fatalf("phys=%v core=%v", phys, core)
			}
			if got.Vendor != tt.want.Vendor || got.Model != tt.want.Model ||
				got.Logical != tt.want.Logical || got.Cores != tt.want.Cores ||
				got.Sockets != tt.want.Sockets || got.ThreadsPerCore != tt.want.ThreadsPerCore {
				t.Fatalf("%+v", got)
			}
			if strings.Join(got.Flags, " ") != strings.Join(tt.want.Flags, " ") {
				t.Fatalf("flags %v", got.Flags)
			}
		})
	}
}

func TestTopologyFallback(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"proc/cpuinfo": "" +
			"processor : 0\nmodel name : test\nflags : fpu\n\n" +
			"processor : 1\nmodel name : test\nflags : fpu\n",
		"sys/devices/system/cpu/cpuidle":                           "",
		"sys/devices/system/cpu/cpu0/topology/physical_package_id": "0\n",
		"sys/devices/system/cpu/cpu0/topology/core_id":             "0\n",
		"sys/devices/system/cpu/cpu1/topology/physical_package_id": "1\n",
		"sys/devices/system/cpu/cpu1/topology/core_id":             "0\n",
	})
	// cpuidle is a file in this fixture; the real kernel uses a directory.
	// cpuIndex must ignore it either way.
	cpu, err := readCPU(root)
	if err != nil {
		t.Fatal(err)
	}
	if cpu.Sockets != 2 || cpu.Cores != 2 || cpu.Logical != 2 || cpu.ThreadsPerCore != 1 {
		t.Fatalf("%+v", cpu)
	}
}

func TestArmVendorUnknown(t *testing.T) {
	if armVendor("0x99") != "0x99" {
		t.Fatal(armVendor("0x99"))
	}
	if armVendor("") != "" {
		t.Fatal("empty implementer")
	}
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
