package envelope

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CPUFreq scales cpuinfo_max_freq / scaling_max_freq when sysfs is writable.
type CPUFreq struct {
	cpus  []string
	maxHz map[string]int64
	base  string
}

// OpenCPUFreq probes /sys/devices/system/cpu. Returns (nil, err) when absent.
func OpenCPUFreq(root string) (*CPUFreq, error) {
	base := "/sys/devices/system/cpu"
	if root != "" {
		base = filepath.Join(root, "sys/devices/system/cpu")
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, fmt.Errorf("not available (%v)", err)
	}
	cf := &CPUFreq{base: base, maxHz: map[string]int64{}}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "cpu") {
			continue
		}
		suffix := strings.TrimPrefix(name, "cpu")
		if _, err := strconv.Atoi(suffix); err != nil {
			continue
		}
		maxPath := filepath.Join(base, name, "cpufreq", "cpuinfo_max_freq")
		raw, err := os.ReadFile(maxPath)
		if err != nil {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		if err != nil || n <= 0 {
			continue
		}
		// Probe writability of scaling_max_freq.
		scale := filepath.Join(base, name, "cpufreq", "scaling_max_freq")
		if err := os.WriteFile(scale, []byte(strconv.FormatInt(n, 10)), 0o644); err != nil {
			continue
		}
		cf.cpus = append(cf.cpus, name)
		cf.maxHz[name] = n
	}
	if len(cf.cpus) == 0 {
		return nil, fmt.Errorf("no writable scaling_max_freq (common on VMs/WSL)")
	}
	return cf, nil
}

func (c *CPUFreq) Name() string { return "cpufreq" }

func (c *CPUFreq) SetLevel(ctx context.Context, level float64) error {
	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	var first error
	for _, cpu := range c.cpus {
		max := c.maxHz[cpu]
		target := int64(float64(max) * level)
		if target < max/20 {
			target = max / 20
		}
		if level <= 0 {
			target = max / 20
		}
		path := filepath.Join(c.base, cpu, "cpufreq", "scaling_max_freq")
		if err := os.WriteFile(path, []byte(strconv.FormatInt(target, 10)), 0o644); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (c *CPUFreq) Close() error {
	return c.SetLevel(context.Background(), 1)
}
