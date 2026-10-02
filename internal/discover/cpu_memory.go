package discover

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
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
		packages := map[string]struct{}{}
		physCore := map[string]struct{}{}
		curPhys := ""
		curCore := ""
		perPackageCores := 0
		for sc.Scan() {
			line := sc.Text()
			if strings.TrimSpace(line) == "" {
				if curPhys != "" && curCore != "" {
					physCore[curPhys+"/"+curCore] = struct{}{}
				}
				curPhys, curCore = "", ""
				continue
			}
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
			if strings.HasPrefix(line, "physical id") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					curPhys = strings.TrimSpace(parts[1])
					packages[curPhys] = struct{}{}
				}
			}
			if strings.HasPrefix(line, "core id") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					curCore = strings.TrimSpace(parts[1])
				}
			}
			if strings.HasPrefix(line, "cpu cores") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					if n, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
						perPackageCores = n
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
		if curPhys != "" && curCore != "" {
			physCore[curPhys+"/"+curCore] = struct{}{}
		}
		if processors > 0 {
			attrs["logical_cpus"] = processors
		}
		switch {
		case len(physCore) > 0:
			cores = len(physCore)
		case perPackageCores > 0 && len(packages) > 0:
			cores = perPackageCores * len(packages)
		case perPackageCores > 0:
			cores = perPackageCores
		case processors > 0:
			cores = processors
		}
		if len(packages) > 0 {
			attrs["sockets"] = len(packages)
		}
	}

	if arch == "" {
		arch = machineArch(root)
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

// machineArch returns a uname-style machine string for the inspected root.
func machineArch(root string) string {
	if m, err := readTrim(pathJoin(root, "/proc/sys/kernel/arch")); err == nil && m != "" {
		return m
	}
	switch {
	case fileExists(pathJoin(root, "/lib/ld-linux-x86-64.so.2")),
		fileExists(pathJoin(root, "/lib64/ld-linux-x86-64.so.2")):
		return "x86_64"
	case fileExists(pathJoin(root, "/lib/ld-linux-aarch64.so.1")):
		return "aarch64"
	}
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "386":
		return "i386"
	default:
		if runtime.GOARCH != "" {
			return runtime.GOARCH
		}
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
