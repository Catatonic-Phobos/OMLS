// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

// Package rdl is the OMLS Resource Description Language, version 0.1.
//
// An RDL document is a JSON inventory of one Linux node. Version 0.1 describes
// what stock userspace can see: operating system, CPU, memory, block devices,
// selected mounts, PCI accelerators, and NUMA nodes. It does not describe
// workloads, models, or scheduling.
package rdl

import (
	"bytes"
	"encoding/json"
	"io"
	"time"
)

const (
	// Version is the schema identifier stored in Document.RDL.
	Version = "0.1"
	// KindNodeInventory is the only kind defined by RDL 0.1.
	KindNodeInventory = "NodeInventory"
)

// Document is one RDL 0.1 node inventory.
type Document struct {
	RDL        string    `json:"rdl"`
	Kind       string    `json:"kind"`
	ObservedAt time.Time `json:"observedAt"`
	Node       Node      `json:"node"`
}

// Node is the machine that was inventoried.
type Node struct {
	Hostname     string        `json:"hostname"`
	MachineID    string        `json:"machineId,omitempty"`
	BootID       string        `json:"bootId,omitempty"`
	OS           OS            `json:"os"`
	CPU          CPU           `json:"cpu"`
	Memory       Memory        `json:"memory"`
	BlockDevices []BlockDevice `json:"blockDevices,omitempty"`
	Mounts       []Mount       `json:"mounts,omitempty"`
	Accelerators []Accelerator `json:"accelerators,omitempty"`
	NUMA         []NUMANode    `json:"numa,omitempty"`
}

// OS identifies the kernel and the userspace distribution.
type OS struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name,omitempty"`
	Version    string `json:"version,omitempty"`
	VersionID  string `json:"versionId,omitempty"`
	PrettyName string `json:"prettyName,omitempty"`
	Kernel     string `json:"kernel"`
	Arch       string `json:"arch"`
}

// CPU is the processor inventory. Flags come from the first logical CPU.
type CPU struct {
	Vendor         string   `json:"vendor,omitempty"`
	Model          string   `json:"model,omitempty"`
	Architecture   string   `json:"architecture,omitempty"`
	Logical        int      `json:"logical"`
	Cores          int      `json:"cores"`
	Sockets        int      `json:"sockets"`
	ThreadsPerCore int      `json:"threadsPerCore,omitempty"`
	Flags          []string `json:"flags,omitempty"`
}

// Memory sizes are bytes. AvailableBytes is MemAvailable, or MemFree when
// MemAvailable is absent. Linux reports those values in KiB; producers convert
// with 1024.
type Memory struct {
	TotalBytes     int64 `json:"totalBytes"`
	AvailableBytes int64 `json:"availableBytes"`
	SwapTotalBytes int64 `json:"swapTotalBytes"`
}

// BlockDevice is a whole disk. SizeBytes is the sysfs sector count times 512.
type BlockDevice struct {
	Name       string `json:"name"`
	SizeBytes  int64  `json:"sizeBytes"`
	Rotational bool   `json:"rotational"`
	Removable  bool   `json:"removable"`
	Model      string `json:"model,omitempty"`
}

// Mount is a filesystem mount kept by the 0.1 persistent-filesystem list.
type Mount struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	FSType  string `json:"fstype"`
	Options string `json:"options,omitempty"`
}

// Accelerator is a PCI display controller (class 0x03) or processing
// accelerator (class 0x12). Vendor is a short name when the vendor id is
// known; VendorID remains the source of truth.
type Accelerator struct {
	Bus      string `json:"bus"`
	Address  string `json:"address"`
	Vendor   string `json:"vendor,omitempty"`
	VendorID string `json:"vendorId"`
	DeviceID string `json:"deviceId"`
	Class    string `json:"class"`
	NUMANode *int   `json:"numaNode,omitempty"`
}

// NUMANode is one node under /sys/devices/system/node. CPUs is the cpulist
// text, not an expanded set.
type NUMANode struct {
	ID          int    `json:"id"`
	CPUs        string `json:"cpus"`
	MemoryBytes int64  `json:"memoryBytes"`
}

// Marshal encodes doc as UTF-8 JSON. compact selects one line; otherwise the
// document is indented with two spaces. The result ends with a newline.
func Marshal(doc Document, compact bool) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if !compact {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Encode writes Marshal output to w.
func Encode(w io.Writer, doc Document, compact bool) error {
	b, err := Marshal(doc, compact)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// Decode reads one JSON document. Unknown fields are ignored so a later
// revision can add fields without breaking a 0.1 reader. Call Validate before
// treating the result as RDL 0.1.
func Decode(r io.Reader) (Document, error) {
	var doc Document
	dec := json.NewDecoder(r)
	if err := dec.Decode(&doc); err != nil {
		return Document{}, err
	}
	return doc, nil
}
