// Package telemetry collects light host samples for fabric heartbeats.
package telemetry

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/graph"
)

// Sample reads best-effort host telemetry. Missing sensors leave TemperatureKnown false.
func Sample(sysRoot string) graph.Telemetry {
	root := strings.TrimRight(sysRoot, "/")
	tel := graph.Telemetry{ObservedAt: time.Now().UTC()}

	if load, ok := readLoadavg(pathJoin(root, "/proc/loadavg")); ok {
		tel.CPULoad = load
	}
	total, avail, ok := readMem(pathJoin(root, "/proc/meminfo"))
	if ok {
		tel.MemTotalBytes = total
		tel.MemAvailableBytes = avail
	}
	if temp, ok := readTempC(root); ok {
		tel.TemperatureC = temp
		tel.TemperatureKnown = true
	}
	return tel
}

func pathJoin(root, p string) string {
	if root == "" {
		return p
	}
	return filepath.Join(root, strings.TrimPrefix(p, "/"))
}

func readLoadavg(path string) (float64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(b))
	if len(fields) < 1 {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func readMem(path string) (total, available int64, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var haveTotal, haveAvail bool
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		n, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		bytes := n * 1024
		switch key {
		case "MemTotal":
			total = bytes
			haveTotal = true
		case "MemAvailable":
			available = bytes
			haveAvail = true
		case "MemFree":
			if !haveAvail {
				available = bytes
				haveAvail = true
			}
		}
	}
	return total, available, haveTotal
}

func readTempC(root string) (float64, bool) {
	thermal := pathJoin(root, "/sys/class/thermal")
	entries, err := os.ReadDir(thermal)
	if err != nil {
		return readHwmonTemp(root)
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "thermal_zone") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(thermal, e.Name(), "temp"))
		if err != nil {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		if err != nil {
			continue
		}
		// thermal zones report millidegrees Celsius.
		return float64(n) / 1000.0, true
	}
	return readHwmonTemp(root)
}

func readHwmonTemp(root string) (float64, bool) {
	base := pathJoin(root, "/sys/class/hwmon")
	entries, err := os.ReadDir(base)
	if err != nil {
		return 0, false
	}
	for _, e := range entries {
		dir := filepath.Join(base, e.Name())
		matches, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
		for _, m := range matches {
			raw, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
			if err != nil {
				continue
			}
			return float64(n) / 1000.0, true
		}
	}
	return 0, false
}
