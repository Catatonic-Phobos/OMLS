// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package rdl

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func validDoc() Document {
	return Document{
		RDL:        Version,
		Kind:       KindNodeInventory,
		ObservedAt: time.Date(2026, 10, 2, 4, 28, 0, 0, time.UTC),
		Node: Node{
			Hostname: "n",
			OS:       OS{Kernel: "6.8.0", Arch: "x86_64"},
			CPU:      CPU{Logical: 1, Cores: 1, Sockets: 1, ThreadsPerCore: 1},
			Memory:   Memory{TotalBytes: 1024},
		},
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	doc := validDoc()
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, compact := range []bool{false, true} {
		payload, err := Marshal(doc, compact)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasSuffix(payload, []byte("\n")) {
			t.Fatalf("missing newline: %q", payload)
		}
		if compact && bytes.Count(payload, []byte("\n")) != 1 {
			t.Fatalf("compact form: %s", payload)
		}
		got, err := Decode(bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		if !got.ObservedAt.Equal(doc.ObservedAt) {
			t.Fatalf("time %s", got.ObservedAt)
		}
		got.ObservedAt = doc.ObservedAt
		if got.Node.Hostname != doc.Node.Hostname || got.RDL != doc.RDL || got.Node.CPU.Logical != 1 {
			t.Fatalf("%+v", got)
		}
	}
}

func TestDecodeIgnoresUnknownFields(t *testing.T) {
	payload, err := Marshal(validDoc(), true)
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.TrimSuffix(string(payload), "\n")
	raw = raw[:len(raw)-1] + `,"future":true}`
	got, err := Decode(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Document)
		wantErr string
	}{
		{name: "ok", mutate: func(*Document) {}},
		{name: "version", mutate: func(d *Document) { d.RDL = "0.2" }, wantErr: "rdl:"},
		{name: "kind", mutate: func(d *Document) { d.Kind = "Pod" }, wantErr: "kind:"},
		{name: "time", mutate: func(d *Document) { d.ObservedAt = time.Time{} }, wantErr: "observedAt"},
		{name: "host", mutate: func(d *Document) { d.Node.Hostname = " " }, wantErr: "hostname"},
		{name: "memory", mutate: func(d *Document) { d.Node.Memory.AvailableBytes = 99999 }, wantErr: "availableBytes"},
		{name: "threads", mutate: func(d *Document) { d.Node.CPU.ThreadsPerCore = 4 }, wantErr: "threadsPerCore"},
		{
			name: "mount",
			mutate: func(d *Document) {
				d.Node.Mounts = []Mount{{Source: "tmpfs", Target: "/tmp", FSType: "tmpfs"}}
			},
			wantErr: "fstype",
		},
		{
			name: "duplicate disk",
			mutate: func(d *Document) {
				d.Node.BlockDevices = []BlockDevice{{Name: "vda"}, {Name: "vda"}}
			},
			wantErr: "duplicate",
		},
		{
			name: "accel",
			mutate: func(d *Document) {
				d.Node.Accelerators = []Accelerator{{
					Bus: "pci", Address: "0000:01:00.0", VendorID: "10de", DeviceID: "2684", Class: "020000",
				}}
			},
			wantErr: "class",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := validDoc()
			tt.mutate(&doc)
			err := doc.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestPersistentFilesystem(t *testing.T) {
	if !PersistentFilesystem("ext4") || !PersistentFilesystem("overlay") {
		t.Fatal("expected persistent filesystems")
	}
	if PersistentFilesystem("tmpfs") || PersistentFilesystem("proc") {
		t.Fatal("pseudo filesystem was accepted")
	}
}
