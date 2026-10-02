package discover

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func collectThermal(root string) ([]rdl.Resource, []string) {
	warns := []string{}
	out := []rdl.Resource{}

	// thermal zones
	tzBase := pathJoin(root, "/sys/class/thermal")
	if entries, err := os.ReadDir(tzBase); err != nil {
		warns = append(warns, "thermal sysfs unavailable: "+err.Error())
	} else {
		idx := 0
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "thermal_zone") {
				continue
			}
			dev := filepath.Join(tzBase, name)
			attrs := map[string]any{"name": name}
			if t, err := readTrim(filepath.Join(dev, "type")); err == nil {
				attrs["type"] = t
			}
			if temp, err := readTrim(filepath.Join(dev, "temp")); err == nil {
				if n, err := strconv.ParseInt(temp, 10, 64); err == nil {
					attrs["temp_millicelsius"] = n
				}
			}
			id := "thermal" + strconv.Itoa(idx)
			idx++
			out = append(out, rdl.Resource{
				ID:           id,
				Kind:         "device",
				Class:        "thermal",
				Capabilities: []string{"thermal"},
				Attrs:        attrs,
			})
		}
	}

	// hwmon sensors
	hwBase := pathJoin(root, "/sys/class/hwmon")
	if entries, err := os.ReadDir(hwBase); err != nil {
		warns = append(warns, "hwmon sysfs unavailable (common on WSL): "+err.Error())
	} else {
		idx := 0
		for _, e := range entries {
			name := e.Name()
			dev := filepath.Join(hwBase, name)
			// Resolve symlink target name if needed
			attrs := map[string]any{"sysfs": name}
			if n, err := readTrim(filepath.Join(dev, "name")); err == nil && n != "" {
				attrs["name"] = n
			}
			temps := map[string]any{}
			files, _ := os.ReadDir(dev)
			for _, f := range files {
				fn := f.Name()
				if strings.HasPrefix(fn, "temp") && strings.HasSuffix(fn, "_input") {
					if v, err := readTrim(filepath.Join(dev, fn)); err == nil {
						if n, err := strconv.ParseInt(v, 10, 64); err == nil {
							temps[fn] = n
						}
					}
				}
			}
			if len(temps) > 0 {
				attrs["temps_millicelsius"] = temps
			}
			id := "hwmon" + strconv.Itoa(idx)
			idx++
			out = append(out, rdl.Resource{
				ID:           id,
				Kind:         "device",
				Class:        "thermal",
				Capabilities: []string{"thermal", "hwmon"},
				Attrs:        attrs,
			})
		}
	}

	if len(out) == 0 {
		warns = append(warns, "no thermal/hwmon sensors discovered")
	}
	return out, warns
}

func collectPower(root string) ([]rdl.Resource, []string) {
	warns := []string{}
	out := []rdl.Resource{}

	pcBase := pathJoin(root, "/sys/class/powercap")
	entries, err := os.ReadDir(pcBase)
	if err != nil {
		warns = append(warns, "powercap sysfs unavailable (common on WSL/VMs): "+err.Error())
		return nil, warns
	}

	idx := 0
	for _, e := range entries {
		name := e.Name()
		dev := filepath.Join(pcBase, name)
		attrs := map[string]any{"sysfs": name}
		if n, err := readTrim(filepath.Join(dev, "name")); err == nil && n != "" {
			attrs["name"] = n
		}
		// RAPL energy/power files if present
		if v, err := readTrim(filepath.Join(dev, "energy_uj")); err == nil {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				attrs["energy_uj"] = n
			}
		}
		if v, err := readTrim(filepath.Join(dev, "max_energy_range_uj")); err == nil {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				attrs["max_energy_range_uj"] = n
			}
		}
		// constraint_0_power_limit_uw etc.
		files, _ := os.ReadDir(dev)
		limits := map[string]any{}
		for _, f := range files {
			fn := f.Name()
			if (strings.Contains(fn, "power_limit") || strings.Contains(fn, "constraint")) && strings.HasSuffix(fn, "_uw") {
				if v, err := readTrim(filepath.Join(dev, fn)); err == nil {
					if n, err := strconv.ParseInt(v, 10, 64); err == nil {
						limits[fn] = n
					}
				}
			}
		}
		if len(limits) > 0 {
			attrs["power_limits_uw"] = limits
		}

		id := "powercap" + strconv.Itoa(idx)
		idx++
		out = append(out, rdl.Resource{
			ID:           id,
			Kind:         "device",
			Class:        "power",
			Capabilities: []string{"power"},
			Attrs:        attrs,
		})
	}
	if len(out) == 0 {
		warns = append(warns, "powercap present but empty")
	}
	return out, warns
}
