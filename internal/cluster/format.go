package cluster

import (
	"fmt"
	"strings"
)

// EnvLabel maps a discovery virt tag to the cluster table's ENV column.
func EnvLabel(virt string) string {
	switch virt {
	case "", "bare":
		return "native"
	case "wsl":
		return "WSL2"
	default:
		return virt
	}
}

// FormatStatus renders `omls status`.
func FormatStatus(s Status) string {
	state := "stopped"
	if s.Running {
		state = "running"
	}
	cluster := s.Cluster
	if cluster == "" {
		cluster = "forming"
	}
	coord := s.Coordinator
	if coord == "" {
		coord = "-"
	}
	return fmt.Sprintf("OMLS: %s\nCluster: %s\nNodes: %d (joined=%d discovered=%d)\nCoordinator: %s\n",
		state, cluster, s.Nodes, s.Joined, s.Discovered, coord)
}

// FormatNodes renders `omls nodes`.
func FormatNodes(nodes []NodeInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-16s %-24s %-10s %s\n", "NODE", "OS", "ENV", "STATUS")
	if len(nodes) == 0 {
		fmt.Fprintf(&b, "(no nodes)\n")
		return b.String()
	}
	for _, n := range nodes {
		name := n.Name
		if name == "" {
			name = n.NodeID
		}
		fmt.Fprintf(&b, "%-16s %-24s %-10s %s\n",
			trunc(name, 16), trunc(n.OS, 24), trunc(n.Env, 10), n.Status)
	}
	return b.String()
}

// FormatCluster renders `omls cluster`.
func FormatCluster(c ClusterInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Cluster: %s\nProtocol: %s\nCoordinator: %s\nRole: %s\nListen: %s\n",
		emptyDash(c.Status.Cluster), emptyDash(c.ProtocolVersion),
		formatCoordinator(c.Status), emptyDash(c.Status.Role), emptyDash(c.Listen))
	if c.Status.NodeID != "" {
		fmt.Fprintf(&b, "Local: %s\n", c.Status.NodeID)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "%-16s %-34s %-10s %s\n", "NODE", "NODE_ID", "ENV", "STATUS")
	for _, n := range c.Members {
		name := n.Name
		if name == "" {
			name = n.NodeID
		}
		fmt.Fprintf(&b, "%-16s %-34s %-10s %s\n",
			trunc(name, 16), n.NodeID, trunc(n.Env, 10), n.Status)
	}
	return b.String()
}

func formatCoordinator(s Status) string {
	if s.Coordinator == "" && s.CoordinatorID == "" {
		return "-"
	}
	if s.CoordinatorID == "" {
		return s.Coordinator
	}
	if s.Coordinator == "" {
		return s.CoordinatorID
	}
	return s.Coordinator + " (" + s.CoordinatorID + ")"
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
