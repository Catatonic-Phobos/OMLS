package cluster

import (
	"fmt"
	"strings"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/graph"
)

// LiveNode is one machine in the live status screen.
type LiveNode struct {
	Name      string
	Status    string
	Logical   int
	CPU       float64
	CPUKnown  bool
	BusyCores float64
	RAM       float64
	RAMKnown  bool
	RAMUsed   int64
	Disk      float64
	DiskKnown bool
	GPU       float64
	GPUKnown  bool
	Temp      float64
	TempKnown bool
	Reserved  int
	Filled    int
}

// LiveView is one frame of `omls status`.
type LiveView struct {
	At          time.Time
	Running     bool
	Cluster     string
	Coordinator string
	Nodes       []LiveNode
}

// FormatLive renders one status frame: per-node usage and how that usage
// and any RAM reservation are split across the cluster.
func FormatLive(v LiveView) string {
	var b strings.Builder
	state := "stopped"
	if v.Running {
		state = "running"
	}
	cluster := v.Cluster
	if cluster == "" {
		cluster = "forming"
	}
	coord := v.Coordinator
	if coord == "" {
		coord = "-"
	}
	stamp := v.At.Format("15:04:05")
	fmt.Fprintf(&b, "OMLS: %s    Cluster: %s    Coordinator: %s    %s\n\n", state, cluster, coord, stamp)
	fmt.Fprintf(&b, "%-12s %7s %7s %7s %7s %7s %10s %10s\n",
		"NODE", "CPU", "RAM", "DISK", "GPU", "TEMP", "RESERVED", "FILLED")
	if len(v.Nodes) == 0 {
		fmt.Fprintf(&b, "(no nodes)\n")
	}
	for _, n := range v.Nodes {
		name := n.Name
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(&b, "%-12s %7s %7s %7s %7s %7s %10s %10s\n",
			trunc(name, 12),
			pct(n.CPU, n.CPUKnown),
			pct(n.RAM, n.RAMKnown),
			pct(n.Disk, n.DiskKnown),
			pct(n.GPU, n.GPUKnown),
			temp(n.Temp, n.TempKnown),
			sizeOrDash(n.Reserved),
			sizeOrDash(n.Filled),
		)
	}
	b.WriteString("\n")
	b.WriteString(formatSplit(v.Nodes))
	b.WriteString("refresh 1s    Ctrl+C to stop\n")
	return b.String()
}

func formatSplit(nodes []LiveNode) string {
	var b strings.Builder
	var busy, ramUsed float64
	var reserved int
	for _, n := range nodes {
		if n.CPUKnown {
			busy += n.BusyCores
		}
		if n.RAMKnown {
			ramUsed += float64(n.RAMUsed)
		}
		reserved += n.Reserved
	}
	fmt.Fprintf(&b, "%-12s %7s %7s %10s\n", "SPLIT", "CPU", "RAM", "RESERVE")
	for _, n := range nodes {
		name := n.Name
		if name == "" {
			name = "-"
		}
		cpuShare := "—"
		if n.CPUKnown && busy > 0 {
			cpuShare = fmt.Sprintf("%.0f%%", n.BusyCores/busy*100)
		}
		ramShare := "—"
		if n.RAMKnown && ramUsed > 0 {
			ramShare = fmt.Sprintf("%.0f%%", float64(n.RAMUsed)/ramUsed*100)
		}
		resShare := "0%"
		if reserved > 0 {
			resShare = fmt.Sprintf("%.0f%%", float64(n.Reserved)/float64(reserved)*100)
		}
		fmt.Fprintf(&b, "%-12s %7s %7s %10s\n", trunc(name, 12), cpuShare, ramShare, resShare)
	}
	if reserved == 0 {
		fmt.Fprintf(&b, "reserve: none\n\n")
	} else {
		fmt.Fprintf(&b, "reserve: %s across %d nodes\n\n", sizeOrDash(reserved), len(nodes))
	}
	return b.String()
}

// UsageFromGraph fills CPU, RAM, temperature, and logical CPUs from a graph node.
// CPU is load divided by logical CPUs. BusyCores is the load itself.
func UsageFromGraph(n graph.NodeEntry) LiveNode {
	out := LiveNode{Name: n.Hostname, Status: string(n.Status), Logical: logicalCPUs(n)}
	if n.Telemetry == nil {
		return out
	}
	tel := n.Telemetry
	if out.Logical < 1 {
		out.Logical = 1
	}
	out.CPU = tel.CPULoad / float64(out.Logical) * 100
	out.CPUKnown = true
	out.BusyCores = tel.CPULoad
	if tel.MemTotalBytes > 0 {
		used := tel.MemTotalBytes - tel.MemAvailableBytes
		if used < 0 {
			used = 0
		}
		out.RAMUsed = used
		out.RAM = float64(used) / float64(tel.MemTotalBytes) * 100
		out.RAMKnown = true
	}
	if tel.TemperatureKnown {
		out.Temp = tel.TemperatureC
		out.TempKnown = true
	}
	return out
}

func logicalCPUs(n graph.NodeEntry) int {
	if n.Profile == nil {
		return 0
	}
	for _, res := range n.Profile.Resources {
		if res.Class != "compute" || res.Attrs == nil {
			continue
		}
		if v, ok := asFloat(res.Attrs["logical_cpus"]); ok && v >= 1 {
			return int(v)
		}
	}
	return 0
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

func pct(v float64, ok bool) string {
	if !ok {
		return "—"
	}
	if v < 0 {
		v = 0
	}
	return fmt.Sprintf("%.0f%%", v)
}

func temp(v float64, ok bool) string {
	if !ok {
		return "—"
	}
	return fmt.Sprintf("%.0f°C", v)
}

func sizeOrDash(n int) string {
	if n <= 0 {
		return "0"
	}
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
