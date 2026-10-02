// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package discover

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func readNUMA(root string) ([]rdl.NUMANode, error) {
	dir := join(root, "sys/devices/system/node")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("sys/devices/system/node: %w", err)
	}
	var out []rdl.NUMANode
	for _, entry := range entries {
		id, ok := numaID(entry.Name())
		if !ok {
			continue
		}
		base := filepath.Join(dir, entry.Name())
		info, err := os.Stat(base)
		if err != nil || !info.IsDir() {
			continue
		}
		cpus, err := readTrim(filepath.Join(base, "cpulist"))
		if err != nil {
			return nil, fmt.Errorf("numa %s: %w", entry.Name(), err)
		}
		if cpus == "" {
			return nil, fmt.Errorf("numa %s: cpulist is empty", entry.Name())
		}
		memText, err := readRequiredFile(filepath.Join(base, "meminfo"))
		if err != nil {
			return nil, fmt.Errorf("numa %s: %w", entry.Name(), err)
		}
		mem, ok, err := parseKB(memText, "MemTotal")
		if err != nil {
			return nil, fmt.Errorf("numa %s: %w", entry.Name(), err)
		}
		if !ok {
			return nil, fmt.Errorf("numa %s: MemTotal is required", entry.Name())
		}
		out = append(out, rdl.NUMANode{
			ID:          id,
			CPUs:        cpus,
			MemoryBytes: mem,
		})
	}
	slices.SortFunc(out, func(a, b rdl.NUMANode) int {
		return cmp.Compare(a.ID, b.ID)
	})
	return out, nil
}

func numaID(name string) (int, bool) {
	rest, ok := strings.CutPrefix(name, "node")
	if !ok || !allDigits(rest) {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return n, true
}

func readRequiredFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
