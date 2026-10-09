package main

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/cache"
	"github.com/Catatonic-Phobos/OMLS/internal/cluster"
	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

type cpuCounters struct {
	total uint64
	idle  uint64
}

func collectStatus(ctx context.Context, sock string, prev cpuCounters, havePrev bool) (cluster.LiveView, cpuCounters, bool) {
	now := time.Now()
	view := cluster.LiveView{At: now}
	var st cluster.Status
	if err := cluster.GetJSON(ctx, sock, "/v1/status", &st); err == nil {
		view.Running = st.Running
		view.Cluster = st.Cluster
		view.Coordinator = st.Coordinator
	}
	var members []cluster.NodeInfo
	_ = cluster.GetJSON(ctx, sock, "/v1/nodes", &members)
	byName := map[string]cluster.LiveNode{}
	if raw, err := cluster.GetBytes(ctx, sock, "/v1/graph"); err == nil {
		var snap graph.Snapshot
		if yaml.Unmarshal(raw, &snap) == nil {
			for _, n := range snap.Nodes {
				byName[n.Hostname] = cluster.UsageFromGraph(n)
			}
		}
	}
	host, _ := os.Hostname()
	cur, cpuOK := readCPU()
	localCPU, localKnown := 0.0, false
	if cpuOK && havePrev && cur.total > prev.total {
		delta := cur.total - prev.total
		idle := cur.idle - prev.idle
		if idle > delta {
			idle = delta
		}
		localCPU = float64(delta-idle) / float64(delta) * 100
		localKnown = true
	}
	disk, diskOK := diskUsedPercent("/")
	gpu, gpuOK := gpuMemoryPercent()
	reserved := cacheStats(ctx, members)

	if len(members) == 0 {
		for _, n := range byName {
			view.Nodes = append(view.Nodes, n)
		}
	}
	for _, m := range members {
		name := m.Name
		if name == "" {
			name = m.NodeID
		}
		n, ok := byName[name]
		if !ok {
			n = cluster.LiveNode{Name: name, Status: m.Status}
		}
		n.Name = name
		if m.Status != "" {
			n.Status = m.Status
		}
		if strings.EqualFold(name, host) {
			if localKnown {
				n.CPU = localCPU
				n.CPUKnown = true
				if n.Logical < 1 {
					n.Logical = 1
				}
				n.BusyCores = localCPU / 100 * float64(n.Logical)
			}
			if diskOK {
				n.Disk = disk
				n.DiskKnown = true
			}
			if gpuOK {
				n.GPU = gpu
				n.GPUKnown = true
			}
		}
		if c, ok := reserved[name]; ok {
			n.Reserved = c.capacity
			n.Filled = c.used
		}
		view.Nodes = append(view.Nodes, n)
	}
	return view, cur, cpuOK
}

type cacheHold struct {
	capacity int
	used     int
}

func cacheStats(ctx context.Context, members []cluster.NodeInfo) map[string]cacheHold {
	out := map[string]cacheHold{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, m := range members {
		if m.Addr == "" {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.NodeID
		}
		wg.Add(1)
		go func(name, addr string) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
			defer cancel()
			st, err := cache.FetchStat(cctx, cache.CachePort(addr))
			if err != nil || st.Capacity == 0 {
				return
			}
			mu.Lock()
			out[name] = cacheHold{capacity: st.Capacity, used: st.Used}
			mu.Unlock()
		}(name, m.Addr)
	}
	wg.Wait()
	return out
}

func readCPU() (cpuCounters, bool) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuCounters{}, false
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
		return cpuCounters{}, false
	}
	var total, idle uint64
	for i := 1; i < len(fields); i++ {
		n, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return cpuCounters{}, false
		}
		total += n
		if i == 4 || i == 5 {
			idle += n
		}
	}
	return cpuCounters{total: total, idle: idle}, true
}

func diskUsedPercent(path string) (float64, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil || st.Blocks == 0 {
		return 0, false
	}
	used := st.Blocks - st.Bavail
	return float64(used) / float64(st.Blocks) * 100, true
}

func gpuMemoryPercent() (float64, bool) {
	out, err := exec.Command("nvidia-smi", "--query-gpu=memory.used,memory.total", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0, false
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	parts := strings.Split(line, ",")
	if len(parts) != 2 {
		return 0, false
	}
	used, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	total, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil || total <= 0 {
		return 0, false
	}
	return used / total * 100, true
}

func isTerminal(fd uintptr) bool {
	_, err := unix.IoctlGetTermios(int(fd), unix.TCGETS)
	return err == nil
}
