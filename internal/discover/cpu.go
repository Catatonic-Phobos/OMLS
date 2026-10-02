// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

package discover

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func readCPU(root string) (rdl.CPU, error) {
	text, err := readRequired(root, "proc/cpuinfo")
	if err != nil {
		return rdl.CPU{}, err
	}
	cpu, havePhys, haveCore, err := parseCPUInfo(text)
	if err != nil {
		return rdl.CPU{}, err
	}
	if !havePhys || !haveCore {
		if err := fillTopology(root, &cpu, !havePhys, !haveCore); err != nil {
			return rdl.CPU{}, err
		}
	}
	return cpu, nil
}

func parseCPUInfo(text string) (cpu rdl.CPU, havePhys, haveCore bool, err error) {
	var cpus []map[string]string
	for _, block := range splitBlocks(text) {
		if _, ok := block["processor"]; !ok {
			continue
		}
		cpus = append(cpus, block)
	}
	if len(cpus) == 0 {
		return rdl.CPU{}, false, false, fmt.Errorf("cpuinfo: no processors")
	}

	cpu.Vendor = firstField(cpus, "vendor_id")
	cpu.Model = firstField(cpus, "model name")
	cpu.Logical = len(cpus)
	if flags := firstField(cpus, "flags"); flags != "" {
		cpu.Flags = strings.Fields(flags)
	} else if features := firstField(cpus, "Features"); features != "" {
		cpu.Flags = strings.Fields(features)
	}
	if cpu.Vendor == "" {
		cpu.Vendor = armVendor(firstField(cpus, "CPU implementer"))
	}
	if cpu.Model == "" {
		if part := firstField(cpus, "CPU part"); part != "" {
			cpu.Model = "CPU part " + part
		}
	}

	packages := map[string]struct{}{}
	cores := map[string]struct{}{}
	for _, proc := range cpus {
		phys := proc["physical id"]
		core := proc["core id"]
		if phys != "" {
			havePhys = true
			packages[phys] = struct{}{}
		}
		if phys != "" && core != "" {
			haveCore = true
			cores[phys+"/"+core] = struct{}{}
		}
	}
	if havePhys {
		cpu.Sockets = len(packages)
	} else {
		cpu.Sockets = 1
	}
	if haveCore {
		cpu.Cores = len(cores)
	} else if n, ok, perr := parsePositive(firstField(cpus, "cpu cores")); perr != nil {
		return rdl.CPU{}, false, false, perr
	} else if ok {
		cpu.Cores = n * cpu.Sockets
		if cpu.Cores > cpu.Logical {
			cpu.Cores = cpu.Logical
		}
	} else {
		cpu.Cores = cpu.Logical
	}
	setThreads(&cpu)
	return cpu, havePhys, haveCore, nil
}

func fillTopology(root string, cpu *rdl.CPU, needSockets, needCores bool) error {
	dir := join(root, "sys/devices/system/cpu")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("cpu topology: %w", err)
	}
	packages := map[string]struct{}{}
	cores := map[string]struct{}{}
	found := 0
	for _, entry := range entries {
		if _, ok := cpuIndex(entry.Name()); !ok {
			continue
		}
		base := filepath.Join(dir, entry.Name(), "topology")
		pkg, err := readTrim(filepath.Join(base, "physical_package_id"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("cpu topology: %w", err)
		}
		core, err := readTrim(filepath.Join(base, "core_id"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("cpu topology: %w", err)
		}
		found++
		packages[pkg] = struct{}{}
		cores[pkg+"/"+core] = struct{}{}
	}
	if found == 0 {
		return nil
	}
	if needSockets && len(packages) > 0 {
		if len(packages) > cpu.Logical {
			return fmt.Errorf("cpu topology: %d sockets for %d logical cpus", len(packages), cpu.Logical)
		}
		cpu.Sockets = len(packages)
	}
	if needCores && len(cores) > 0 {
		if len(cores) > cpu.Logical {
			return fmt.Errorf("cpu topology: %d cores for %d logical cpus", len(cores), cpu.Logical)
		}
		cpu.Cores = len(cores)
	}
	setThreads(cpu)
	return nil
}

func setThreads(cpu *rdl.CPU) {
	cpu.ThreadsPerCore = 0
	if cpu.Cores > 0 && cpu.Logical%cpu.Cores == 0 {
		cpu.ThreadsPerCore = cpu.Logical / cpu.Cores
	}
}

func splitBlocks(text string) []map[string]string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var blocks []map[string]string
	cur := map[string]string{}
	flush := func() {
		if len(cur) == 0 {
			return
		}
		blocks = append(blocks, cur)
		cur = map[string]string{}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		key, val, ok := splitKV(line)
		if !ok || key == "" {
			continue
		}
		cur[key] = val
	}
	flush()
	return blocks
}

func splitKV(line string) (string, string, bool) {
	key, val, ok := strings.Cut(line, ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(val), true
}

func firstField(cpus []map[string]string, key string) string {
	for _, cpu := range cpus {
		if v := strings.TrimSpace(cpu[key]); v != "" {
			return v
		}
	}
	return ""
}

func parsePositive(s string) (int, bool, error) {
	if s == "" {
		return 0, false, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false, fmt.Errorf("cpuinfo: cpu cores %q", s)
	}
	return n, true, nil
}

func cpuIndex(name string) (int, bool) {
	rest, ok := strings.CutPrefix(name, "cpu")
	if !ok || !allDigits(rest) {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return n, true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func armVendor(implementer string) string {
	impl := strings.ToLower(strings.TrimSpace(implementer))
	impl = strings.TrimPrefix(impl, "0x")
	if impl == "" {
		return ""
	}
	switch impl {
	case "41":
		return "ARM"
	case "42":
		return "Broadcom"
	case "43":
		return "Cavium"
	case "44":
		return "DEC"
	case "48":
		return "HiSilicon"
	case "4e":
		return "NVIDIA"
	case "50":
		return "APM"
	case "51":
		return "Qualcomm"
	case "53":
		return "Samsung"
	case "56":
		return "Marvell"
	case "61":
		return "Apple"
	case "6d":
		return "Microsoft"
	default:
		return "0x" + impl
	}
}

func readTrim(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
