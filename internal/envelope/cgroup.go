package envelope

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CgroupCPU drives cgroup v2 cpu.max when the hierarchy is writable.
type CgroupCPU struct {
	path   string
	owned  bool
	period uint64 // microseconds
}

// OpenCgroupCPU tries to create (or reuse) an omls cgroup. Returns (nil, nil) if
// cgroup v2 cpu controller is unavailable; (nil, err) on unexpected failures
// that should become warnings.
func OpenCgroupCPU(base string) (*CgroupCPU, error) {
	if base == "" {
		base = "/sys/fs/cgroup"
	}
	controllers := filepath.Join(base, "cgroup.controllers")
	raw, err := os.ReadFile(controllers)
	if err != nil {
		return nil, fmt.Errorf("not available (%v)", err)
	}
	if !strings.Contains(" "+string(raw)+" ", " cpu ") && !strings.HasPrefix(string(raw), "cpu") && !strings.Contains(string(raw), " cpu") {
		// also accept "cpu" as first token
		ok := false
		for _, f := range strings.Fields(string(raw)) {
			if f == "cpu" {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("cpu controller not in cgroup.controllers")
		}
	}

	// Prefer a dedicated subtree; fall back to current cgroup if we can write cpu.max there.
	omlsDir := filepath.Join(base, "omls")
	_ = os.MkdirAll(omlsDir, 0o755)
	// Enable cpu in subtree when possible (best-effort).
	_ = os.WriteFile(filepath.Join(base, "cgroup.subtree_control"), []byte("+cpu"), 0o644)
	_ = os.WriteFile(filepath.Join(omlsDir, "cgroup.subtree_control"), []byte("+cpu"), 0o644)

	child := filepath.Join(omlsDir, fmt.Sprintf("sess-%d", os.Getpid()))
	if err := os.MkdirAll(child, 0o755); err != nil {
		// try current cgroup
		cur, cerr := currentCgroup(base)
		if cerr != nil {
			return nil, fmt.Errorf("mkdir %s: %v", child, err)
		}
		child = cur
		cg := &CgroupCPU{path: child, owned: false, period: 100000}
		if err := cg.writeMax(1); err != nil {
			return nil, fmt.Errorf("cpu.max not writable: %w", err)
		}
		return cg, nil
	}
	cg := &CgroupCPU{path: child, owned: true, period: 100000}
	// Move our process into the cgroup so cpu.max applies.
	if err := os.WriteFile(filepath.Join(child, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		_ = os.Remove(child)
		return nil, fmt.Errorf("join cgroup: %w", err)
	}
	if err := cg.writeMax(1); err != nil {
		_ = cg.Close()
		return nil, err
	}
	return cg, nil
}

func (c *CgroupCPU) Name() string { return "cgroup-cpu" }

func (c *CgroupCPU) SetLevel(ctx context.Context, level float64) error {
	return c.writeMax(level)
}

func (c *CgroupCPU) writeMax(level float64) error {
	if level <= 0 {
		// Quota 1000us of each period ≈ nearly parked but still schedulable.
		return os.WriteFile(filepath.Join(c.path, "cpu.max"), []byte(fmt.Sprintf("1000 %d", c.period)), 0o644)
	}
	if level >= 1 {
		return os.WriteFile(filepath.Join(c.path, "cpu.max"), []byte("max "+strconv.FormatUint(c.period, 10)), 0o644)
	}
	quota := uint64(float64(c.period) * level)
	if quota < 1000 {
		quota = 1000
	}
	return os.WriteFile(filepath.Join(c.path, "cpu.max"), []byte(fmt.Sprintf("%d %d", quota, c.period)), 0o644)
}

func (c *CgroupCPU) Close() error {
	if c == nil {
		return nil
	}
	_ = c.writeMax(1)
	if c.owned {
		// Move PID back to parent before remove.
		parent := filepath.Dir(c.path)
		_ = os.WriteFile(filepath.Join(parent, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0o644)
		_ = os.Remove(c.path)
	}
	return nil
}

func currentCgroup(base string) (string, error) {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		// cgroup v2: 0::/path
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[1] != "" {
			continue
		}
		rel := parts[2]
		if rel == "/" {
			return base, nil
		}
		return filepath.Join(base, strings.TrimPrefix(rel, "/")), nil
	}
	return "", fmt.Errorf("no cgroup v2 path in /proc/self/cgroup")
}
