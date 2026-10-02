// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package rdl

import (
	"fmt"
	"strconv"
	"strings"
)

// Validate checks the RDL 0.1 invariants. It does not require block devices,
// mounts, accelerators, or NUMA nodes; a node may have none of those.
func (d Document) Validate() error {
	if d.RDL != Version {
		return fmt.Errorf("rdl: want %s, got %q", Version, d.RDL)
	}
	if d.Kind != KindNodeInventory {
		return fmt.Errorf("kind: want %s, got %q", KindNodeInventory, d.Kind)
	}
	if d.ObservedAt.IsZero() {
		return fmt.Errorf("observedAt: required")
	}
	if strings.TrimSpace(d.Node.Hostname) == "" {
		return fmt.Errorf("node.hostname: required")
	}
	if err := d.Node.OS.validate(); err != nil {
		return err
	}
	if err := d.Node.CPU.validate(); err != nil {
		return err
	}
	if err := d.Node.Memory.validate(); err != nil {
		return err
	}
	seenDisk := map[string]struct{}{}
	for i, disk := range d.Node.BlockDevices {
		if err := disk.validate(); err != nil {
			return fmt.Errorf("blockDevices[%d]: %w", i, err)
		}
		if _, ok := seenDisk[disk.Name]; ok {
			return fmt.Errorf("blockDevices: duplicate name %q", disk.Name)
		}
		seenDisk[disk.Name] = struct{}{}
	}
	for i, mnt := range d.Node.Mounts {
		if err := mnt.validate(); err != nil {
			return fmt.Errorf("mounts[%d]: %w", i, err)
		}
	}
	for i, acc := range d.Node.Accelerators {
		if err := acc.validate(); err != nil {
			return fmt.Errorf("accelerators[%d]: %w", i, err)
		}
	}
	seenNUMA := map[int]struct{}{}
	for i, node := range d.Node.NUMA {
		if err := node.validate(); err != nil {
			return fmt.Errorf("numa[%d]: %w", i, err)
		}
		if _, ok := seenNUMA[node.ID]; ok {
			return fmt.Errorf("numa: duplicate id %d", node.ID)
		}
		seenNUMA[node.ID] = struct{}{}
	}
	return nil
}

func (o OS) validate() error {
	if strings.TrimSpace(o.Kernel) == "" {
		return fmt.Errorf("os.kernel: required")
	}
	if strings.TrimSpace(o.Arch) == "" {
		return fmt.Errorf("os.arch: required")
	}
	return nil
}

func (c CPU) validate() error {
	if c.Logical < 1 {
		return fmt.Errorf("cpu.logical: want >= 1, got %d", c.Logical)
	}
	if c.Cores < 1 || c.Cores > c.Logical {
		return fmt.Errorf("cpu.cores: want 1..%d, got %d", c.Logical, c.Cores)
	}
	if c.Sockets < 1 || c.Sockets > c.Logical {
		return fmt.Errorf("cpu.sockets: want 1..%d, got %d", c.Logical, c.Sockets)
	}
	if c.Cores < c.Sockets {
		return fmt.Errorf("cpu.cores: %d is below socket count %d", c.Cores, c.Sockets)
	}
	if c.ThreadsPerCore < 0 {
		return fmt.Errorf("cpu.threadsPerCore: negative")
	}
	if c.ThreadsPerCore > 0 && c.ThreadsPerCore*c.Cores != c.Logical {
		return fmt.Errorf("cpu.threadsPerCore: %d * %d cores != %d logical", c.ThreadsPerCore, c.Cores, c.Logical)
	}
	return nil
}

func (m Memory) validate() error {
	if m.TotalBytes <= 0 {
		return fmt.Errorf("memory.totalBytes: want > 0, got %d", m.TotalBytes)
	}
	if m.AvailableBytes < 0 || m.AvailableBytes > m.TotalBytes {
		return fmt.Errorf("memory.availableBytes: %d outside 0..%d", m.AvailableBytes, m.TotalBytes)
	}
	if m.SwapTotalBytes < 0 {
		return fmt.Errorf("memory.swapTotalBytes: negative")
	}
	return nil
}

func (b BlockDevice) validate() error {
	if b.Name == "" {
		return fmt.Errorf("name: required")
	}
	if b.SizeBytes < 0 {
		return fmt.Errorf("sizeBytes: negative")
	}
	return nil
}

func (m Mount) validate() error {
	if m.Source == "" {
		return fmt.Errorf("source: required")
	}
	if m.Target == "" {
		return fmt.Errorf("target: required")
	}
	if !PersistentFilesystem(m.FSType) {
		return fmt.Errorf("fstype: %q is not an RDL 0.1 filesystem", m.FSType)
	}
	return nil
}

func (a Accelerator) validate() error {
	if a.Bus != "pci" {
		return fmt.Errorf("bus: want pci, got %q", a.Bus)
	}
	if a.Address == "" {
		return fmt.Errorf("address: required")
	}
	if !hexDigits(a.VendorID, 4) {
		return fmt.Errorf("vendorId: want 4 hex digits, got %q", a.VendorID)
	}
	if !hexDigits(a.DeviceID, 4) {
		return fmt.Errorf("deviceId: want 4 hex digits, got %q", a.DeviceID)
	}
	if !acceleratorClass(a.Class) {
		return fmt.Errorf("class: want display or processing accelerator, got %q", a.Class)
	}
	if a.NUMANode != nil && *a.NUMANode < 0 {
		return fmt.Errorf("numaNode: negative")
	}
	return nil
}

func (n NUMANode) validate() error {
	if n.ID < 0 {
		return fmt.Errorf("id: negative")
	}
	if strings.TrimSpace(n.CPUs) == "" {
		return fmt.Errorf("cpus: required")
	}
	if n.MemoryBytes < 0 {
		return fmt.Errorf("memoryBytes: negative")
	}
	return nil
}

func hexDigits(s string, n int) bool {
	if len(s) != n {
		return false
	}
	_, err := strconv.ParseUint(s, 16, n*4)
	return err == nil
}

func acceleratorClass(class string) bool {
	if len(class) != 6 {
		return false
	}
	v, err := strconv.ParseUint(class, 16, 24)
	if err != nil {
		return false
	}
	base := v >> 16
	return base == 0x03 || base == 0x12
}
