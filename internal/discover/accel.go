// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package discover

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

var pciVendors = map[string]string{
	"10de": "nvidia",
	"1002": "amd",
	"1022": "amd",
	"8086": "intel",
}

func readAccelerators(root string) ([]rdl.Accelerator, error) {
	dir := join(root, "sys/bus/pci/devices")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("sys/bus/pci/devices: %w", err)
	}
	var out []rdl.Accelerator
	for _, entry := range entries {
		addr := entry.Name()
		base := filepath.Join(dir, addr)
		info, err := os.Stat(base)
		if err != nil || !info.IsDir() {
			continue
		}
		classText, err := readTrim(filepath.Join(base, "class"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("pci %s: %w", addr, err)
		}
		class, err := parsePCIValue(classText)
		if err != nil || class > 0xffffff {
			return nil, fmt.Errorf("pci %s: class %q", addr, classText)
		}
		baseClass := (class >> 16) & 0xff
		if baseClass != 0x03 && baseClass != 0x12 {
			continue
		}
		vendorText, err := readTrim(filepath.Join(base, "vendor"))
		if err != nil {
			return nil, fmt.Errorf("pci %s: %w", addr, err)
		}
		deviceText, err := readTrim(filepath.Join(base, "device"))
		if err != nil {
			return nil, fmt.Errorf("pci %s: %w", addr, err)
		}
		vendor, err := parsePCIValue(vendorText)
		if err != nil || vendor > 0xffff {
			return nil, fmt.Errorf("pci %s: vendor %q", addr, vendorText)
		}
		device, err := parsePCIValue(deviceText)
		if err != nil || device > 0xffff {
			return nil, fmt.Errorf("pci %s: device %q", addr, deviceText)
		}
		vendorID := fmt.Sprintf("%04x", vendor)
		numa, err := readAccelNUMA(filepath.Join(base, "numa_node"))
		if err != nil {
			return nil, fmt.Errorf("pci %s: %w", addr, err)
		}
		out = append(out, rdl.Accelerator{
			Bus:      "pci",
			Address:  addr,
			Vendor:   pciVendors[vendorID],
			VendorID: vendorID,
			DeviceID: fmt.Sprintf("%04x", device),
			Class:    fmt.Sprintf("%06x", class),
			NUMANode: numa,
		})
	}
	slices.SortFunc(out, func(a, b rdl.Accelerator) int {
		return strings.Compare(a.Address, b.Address)
	})
	return out, nil
}

func readAccelNUMA(path string) (*int, error) {
	text, err := readTrim(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if text == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return nil, fmt.Errorf("numa_node: %w", err)
	}
	if n < 0 {
		return nil, nil
	}
	return &n, nil
}

func parsePCIValue(s string) (uint32, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 0, 32)
	if err != nil {
		return 0, err
	}
	return uint32(n), nil
}
