package discover

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func collectCPU(root string) ([]rdl.Resource, []string) {
	warns := []string{}
	attrs := map[string]any{}

	arch := ""
	model := ""
	cores := 0
	siblings := 0

	cpuinfo := pathJoin(root, "/proc/cpuinfo")
	if f, err := os.Open(cpuinfo); err != nil {
		warns = append(warns, "/proc/cpuinfo unreadable: "+err.Error())
	} else {
		defer f.Close()
		sc := bufio.NewScanner(f)
		processors := 0
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "processor") {
				processors++
			}
			if arch == "" && strings.HasPrefix(line, "architecture") {
				// rare on x86
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					arch = strings.TrimSpace(parts[1])
				}
			}
			if model == "" && (strings.HasPrefix(line, "model name") || strings.HasPrefix(line, "Hardware") || strings.HasPrefix(line, "cpu model")) {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					model = strings.TrimSpace(parts[1])
				}
			}
			if strings.HasPrefix(line, "cpu cores") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					if n, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
						cores = n
					}
				}
			}
			if strings.HasPrefix(line, "siblings") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					if n, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
						siblings = n
					}
				}
			}
		}
		if processors > 0 {
			attrs["logical_cpus"] = processors
			if cores == 0 {
				cores = processors
			}
		}
	}

	if arch == "" {
		// fall back to uname-ish from /proc
		if m, err := readTrim(pathJoin(root, "/proc/sys/kernel/arch")); err == nil && m != "" {
			arch = m
		}
	}
	if arch == "" {
		arch = guessArch()
	}
	attrs["arch"] = arch
	if model != "" {
		attrs["model"] = model
	}
	if cores > 0 {
		attrs["cores"] = cores
	}
	if siblings > 0 {
		attrs["siblings"] = siblings
	}

	// CPUFreq governors (best-effort).
	govs := map[string]struct{}{}
	cpuBase := pathJoin(root, "/sys/devices/system/cpu")
	entries, err := os.ReadDir(cpuBase)
	if err != nil {
		warns = append(warns, "CPUFreq/sysfs cpu topology unavailable: "+err.Error())
	} else {
		online := 0
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "cpu") {
				continue
			}
			suffix := strings.TrimPrefix(name, "cpu")
			if _, err := strconv.Atoi(suffix); err != nil {
				continue
			}
			online++
			govPath := filepath.Join(cpuBase, name, "cpufreq", "scaling_governor")
			if g, err := readTrim(govPath); err == nil && g != "" {
				govs[g] = struct{}{}
			}
			maxPath := filepath.Join(cpuBase, name, "cpufreq", "cpuinfo_max_freq")
			if maxPath != "" {
				if v, err := readTrim(maxPath); err == nil {
					if n, err := strconv.ParseInt(v, 10, 64); err == nil {
						attrs["cpufreq_max_khz"] = n
					}
				}
			}
		}
		if online > 0 {
			attrs["online_cpus"] = online
		}
		if len(govs) == 0 {
			warns = append(warns, "CPUFreq governors not present (common on WSL/VMs)")
		} else {
			list := make([]string, 0, len(govs))
			for g := range govs {
				list = append(list, g)
			}
			attrs["cpufreq_governors"] = list
		}
	}

	caps := []string{"compute", "parallelizable"}
	return []rdl.Resource{{
		ID:           "cpu0",
		Kind:         "device",
		Class:        "compute",
		Capabilities: caps,
		Attrs:        attrs,
	}}, warns
}

func guessArch() string {
	switch {
	case fileExists("/lib/ld-linux-x86-64.so.2"):
		return "x86_64"
	case fileExists("/lib/ld-linux-aarch64.so.1"):
		return "aarch64"
	default:
		return "unknown"
	}
}

func collectMemory(root string) ([]rdl.Resource, []string) {
	warns := []string{}
	attrs := map[string]any{}
	path := pathJoin(root, "/proc/meminfo")
	f, err := os.Open(path)
	if err != nil {
		warns = append(warns, "/proc/meminfo unreadable: "+err.Error())
		return nil, warns
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		val, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		// meminfo values are kB
		bytes := val * 1024
		switch key {
		case "MemTotal":
			attrs["total_bytes"] = bytes
		case "MemAvailable":
			attrs["available_bytes"] = bytes
		case "MemFree":
			attrs["free_bytes"] = bytes
		case "SwapTotal":
			attrs["swap_total_bytes"] = bytes
		}
	}
	if len(attrs) == 0 {
		warns = append(warns, "memory attrs empty")
		return nil, warns
	}
	return []rdl.Resource{{
		ID:           "mem0",
		Kind:         "device",
		Class:        "memory",
		Capabilities: []string{"memory"},
		Attrs:        attrs,
	}}, warns
}
