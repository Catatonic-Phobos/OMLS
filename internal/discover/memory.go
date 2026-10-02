// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package discover

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func readMemory(root string) (rdl.Memory, error) {
	text, err := readRequired(root, "proc/meminfo")
	if err != nil {
		return rdl.Memory{}, err
	}
	mem, err := parseMeminfo(text)
	if err != nil {
		return rdl.Memory{}, err
	}
	return mem, nil
}

func parseMeminfo(text string) (rdl.Memory, error) {
	total, ok, err := parseKB(text, "MemTotal")
	if err != nil {
		return rdl.Memory{}, err
	}
	if !ok || total <= 0 {
		return rdl.Memory{}, fmt.Errorf("meminfo: MemTotal is required")
	}
	available, ok, err := parseKB(text, "MemAvailable")
	if err != nil {
		return rdl.Memory{}, err
	}
	if !ok {
		available, _, err = parseKB(text, "MemFree")
		if err != nil {
			return rdl.Memory{}, err
		}
	}
	swap, _, err := parseKB(text, "SwapTotal")
	if err != nil {
		return rdl.Memory{}, err
	}
	return rdl.Memory{
		TotalBytes:     total,
		AvailableBytes: available,
		SwapTotalBytes: swap,
	}, nil
}

// parseKB returns the named meminfo field in bytes. Linux labels the unit kB
// and means 1024 bytes.
func parseKB(text, key string) (int64, bool, error) {
	token := key + ":"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		idx := strings.Index(line, token)
		if idx < 0 {
			continue
		}
		if idx > 0 && line[idx-1] != ' ' {
			continue
		}
		rest := strings.TrimSpace(line[idx+len(token):])
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0, false, fmt.Errorf("meminfo %s: missing value", key)
		}
		if len(fields) >= 2 && fields[1] != "kB" {
			return 0, false, fmt.Errorf("meminfo %s: unit %q", key, fields[1])
		}
		n, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0, false, fmt.Errorf("meminfo %s: %w", key, err)
		}
		if n < 0 {
			return 0, false, fmt.Errorf("meminfo %s: negative", key)
		}
		return n * 1024, true, nil
	}
	return 0, false, nil
}
