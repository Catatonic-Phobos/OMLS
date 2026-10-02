// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package discover

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func readBlocks(root string) ([]rdl.BlockDevice, error) {
	dir := join(root, "sys/block")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("sys/block: %w", err)
	}
	var out []rdl.BlockDevice
	for _, entry := range entries {
		name := entry.Name()
		if skipBlock(name) {
			continue
		}
		base := filepath.Join(dir, name)
		info, err := os.Stat(base)
		if err != nil || !info.IsDir() {
			continue
		}
		hidden, err := readTrim(filepath.Join(base, "hidden"))
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("block %s: %w", name, err)
		}
		if hidden == "1" {
			continue
		}
		sizeText, err := readTrim(filepath.Join(base, "size"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("block %s: %w", name, err)
		}
		sectors, err := strconv.ParseInt(sizeText, 10, 64)
		if err != nil || sectors < 0 {
			return nil, fmt.Errorf("block %s: size %q", name, sizeText)
		}
		if sectors > math.MaxInt64/512 {
			return nil, fmt.Errorf("block %s: size overflows", name)
		}
		rot, err := readFlag(filepath.Join(base, "queue", "rotational"))
		if err != nil {
			return nil, fmt.Errorf("block %s: %w", name, err)
		}
		rem, err := readFlag(filepath.Join(base, "removable"))
		if err != nil {
			return nil, fmt.Errorf("block %s: %w", name, err)
		}
		model, err := readTrim(filepath.Join(base, "device", "model"))
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("block %s: %w", name, err)
		}
		out = append(out, rdl.BlockDevice{
			Name:       name,
			SizeBytes:  sectors * 512,
			Rotational: rot,
			Removable:  rem,
			Model:      model,
		})
	}
	slices.SortFunc(out, func(a, b rdl.BlockDevice) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

func skipBlock(name string) bool {
	for _, prefix := range []string{"loop", "ram", "fd", "nbd", "zram", "sr"} {
		rest, ok := strings.CutPrefix(name, prefix)
		if ok && allDigits(rest) {
			return true
		}
	}
	return false
}

func readFlag(path string) (bool, error) {
	text, err := readTrim(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	switch text {
	case "", "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, fmt.Errorf("%s: want 0 or 1, got %q", path, text)
	}
}
