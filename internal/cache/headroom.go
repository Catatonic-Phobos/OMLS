package cache

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// Headroom is one machine's published CPU, RAM, and VRAM.
// Every peer serves this on /v1/offer so a borrow decision does not ask the coordinator.
type Headroom struct {
	Hostname  string  `json:"hostname"`
	CPU       float64 `json:"cpu"`
	CPUKnown  bool    `json:"cpu_known"`
	RAM       float64 `json:"ram"`
	RAMKnown  bool    `json:"ram_known"`
	RAMFree   int64   `json:"ram_free"`
	GPU       float64 `json:"gpu"`
	GPUKnown  bool    `json:"gpu_known"`
	VRAMFree  int64   `json:"vram_free"`
	VRAMKnown bool    `json:"vram_known"`
}

var (
	cpuMu   sync.Mutex
	prevCPU sampleCPU
	haveCPU bool
)

type sampleCPU struct {
	total uint64
	idle  uint64
}

// SampleHeadroom reads this machine. CPU becomes known on the second call,
// using the same /proc/stat delta as the status screen.
func SampleHeadroom() Headroom {
	host, _ := os.Hostname()
	h := Headroom{Hostname: host}
	if pct, ok := cpuPercent(); ok {
		h.CPU = pct
		h.CPUKnown = true
	}
	if total, free, ok := memBytes(); ok && total > 0 {
		used := total - free
		if used < 0 {
			used = 0
		}
		h.RAM = float64(used) / float64(total) * 100
		h.RAMKnown = true
		h.RAMFree = free
	}
	if pct, free, ok := vram(); ok {
		h.GPU = pct
		h.GPUKnown = true
		h.VRAMFree = free
		h.VRAMKnown = true
	}
	return h
}

func cpuPercent() (float64, bool) {
	cur, ok := readProcStat()
	if !ok {
		return 0, false
	}
	cpuMu.Lock()
	defer cpuMu.Unlock()
	prev := prevCPU
	known := haveCPU && cur.total > prev.total
	prevCPU = cur
	haveCPU = true
	if !known {
		return 0, false
	}
	delta := cur.total - prev.total
	idle := cur.idle - prev.idle
	if idle > delta {
		idle = delta
	}
	return float64(delta-idle) / float64(delta) * 100, true
}

func readProcStat() (sampleCPU, bool) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return sampleCPU{}, false
	}
	line := ""
	for _, ln := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(ln, "cpu ") {
			line = ln
			break
		}
	}
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return sampleCPU{}, false
	}
	var total, idle uint64
	for i := 1; i < len(fields); i++ {
		n, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return sampleCPU{}, false
		}
		total += n
		if i == 4 || i == 5 {
			idle += n
		}
	}
	return sampleCPU{total: total, idle: idle}, true
}

func memBytes() (total, free int64, ok bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var haveTotal, haveFree bool
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			total = n * 1024
			haveTotal = true
		case "MemAvailable":
			free = n * 1024
			haveFree = true
		}
	}
	return total, free, haveTotal && haveFree
}

func vram() (usedPct float64, free int64, ok bool) {
	out, err := exec.Command("nvidia-smi", "--query-gpu=memory.used,memory.total", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0, 0, false
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	parts := strings.Split(line, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	used, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	total, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil || total <= 0 {
		return 0, 0, false
	}
	freeMiB := total - used
	if freeMiB < 0 {
		freeMiB = 0
	}
	return used / total * 100, int64(freeMiB) * 1024 * 1024, true
}
