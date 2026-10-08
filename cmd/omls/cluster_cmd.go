package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/cluster"
	"github.com/Catatonic-Phobos/OMLS/internal/envelope"
)

func runDaemon(args []string) error {
	cfg := cluster.Config{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--data-dir":
			i++
			if i >= len(args) {
				return fmt.Errorf("--data-dir requires a path")
			}
			cfg.DataDir = args[i]
		case strings.HasPrefix(a, "--data-dir="):
			cfg.DataDir = strings.TrimPrefix(a, "--data-dir=")
		case a == "--listen":
			i++
			if i >= len(args) {
				return fmt.Errorf("--listen requires addr")
			}
			cfg.Listen = args[i]
		case strings.HasPrefix(a, "--listen="):
			cfg.Listen = strings.TrimPrefix(a, "--listen=")
		case a == "--socket":
			i++
			if i >= len(args) {
				return fmt.Errorf("--socket requires a path")
			}
			cfg.SocketPath = args[i]
		case strings.HasPrefix(a, "--socket="):
			cfg.SocketPath = strings.TrimPrefix(a, "--socket=")
		case a == "--interval":
			i++
			if i >= len(args) {
				return fmt.Errorf("--interval requires a duration")
			}
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return err
			}
			cfg.Interval = d
		case strings.HasPrefix(a, "--interval="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--interval="))
			if err != nil {
				return err
			}
			cfg.Interval = d
		case a == "--heartbeat-timeout":
			i++
			if i >= len(args) {
				return fmt.Errorf("--heartbeat-timeout requires a duration")
			}
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return err
			}
			cfg.HeartbeatTimeout = d
		case strings.HasPrefix(a, "--heartbeat-timeout="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--heartbeat-timeout="))
			if err != nil {
				return err
			}
			cfg.HeartbeatTimeout = d
		case a == "--community-dir":
			i++
			if i >= len(args) {
				return fmt.Errorf("--community-dir requires a path")
			}
			cfg.CommunityDir = args[i]
		case strings.HasPrefix(a, "--community-dir="):
			cfg.CommunityDir = strings.TrimPrefix(a, "--community-dir=")
		case a == "-h" || a == "--help":
			fmt.Print(daemonUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cluster.Run(ctx, cfg)
}

func runClusterStatus(args []string) error {
	if helpRequested(args) {
		fmt.Print(clusterStatusUsage())
		return nil
	}
	sock, err := parseSocketArgs(args)
	if err != nil {
		return daemonClientErr(err)
	}
	var st cluster.Status
	if err := cluster.GetJSON(context.Background(), sock, "/v1/status", &st); err != nil {
		return daemonClientErr(err)
	}
	fmt.Print(cluster.FormatStatus(st))
	return nil
}

func runClusterNodes(args []string) error {
	if helpRequested(args) {
		fmt.Print(clusterNodesUsage())
		return nil
	}
	sock, err := parseSocketArgs(args)
	if err != nil {
		return daemonClientErr(err)
	}
	var nodes []cluster.NodeInfo
	if err := cluster.GetJSON(context.Background(), sock, "/v1/nodes", &nodes); err != nil {
		return daemonClientErr(err)
	}
	fmt.Print(cluster.FormatNodes(nodes))
	return nil
}

func runClusterInfo(args []string) error {
	if helpRequested(args) {
		fmt.Print(clusterInfoUsage())
		return nil
	}
	sock, err := parseSocketArgs(args)
	if err != nil {
		return daemonClientErr(err)
	}
	var info cluster.ClusterInfo
	if err := cluster.GetJSON(context.Background(), sock, "/v1/cluster", &info); err != nil {
		return daemonClientErr(err)
	}
	fmt.Print(cluster.FormatCluster(info))
	return nil
}

func runClusterGraph(args []string) error {
	if helpRequested(args) {
		fmt.Print(clusterGraphUsage())
		return nil
	}
	sock, out, err := parseGraphArgs(args)
	if err != nil {
		return daemonClientErr(err)
	}
	raw, err := cluster.GetBytes(context.Background(), sock, "/v1/graph")
	if err != nil {
		return daemonClientErr(err)
	}
	if out == "" {
		_, err = os.Stdout.Write(raw)
		if len(raw) == 0 || raw[len(raw)-1] != '\n' {
			fmt.Println()
		}
		return err
	}
	if err := os.WriteFile(out, raw, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", out, len(raw))
	return nil
}

func runClusterDemo(args []string) error {
	if helpRequested(args) {
		fmt.Print(clusterDemoUsage())
		return nil
	}
	sock, req, err := parseDemoArgs(args)
	if err != nil {
		return daemonClientErr(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(req.TimeoutMs)*time.Millisecond)
	defer cancel()
	var demo cluster.DemoResult
	if err := cluster.PostJSON(ctx, sock, "/v1/run-demo", req, &demo); err != nil {
		return daemonClientErr(err)
	}
	for _, r := range demo.Rounds {
		fmt.Printf("\n=== round %d (%d ms) ===\n", r.Round, r.WallMs)
		fmt.Printf("policy: %s\n", r.PolicyNote)
		for _, a := range r.Allocations {
			fmt.Printf("  %-12s workers=%d score=%.1f virt=%s host=%s\n",
				short(a.NodeID), a.Workers, a.Score, a.Virt, a.Hostname)
			if a.Reason != "" {
				fmt.Printf("             reason: %s\n", a.Reason)
			}
		}
		if len(r.DurationEwmaMs) > 0 {
			fmt.Printf("  duration_ewma_ms: %v\n", r.DurationEwmaMs)
		}
	}
	fmt.Printf("\nsummary: %s\n", demo.Summary)
	return nil
}

func parseSocketArgs(args []string) (string, error) {
	explicit := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--socket":
			i++
			if i >= len(args) {
				return "", fmt.Errorf("--socket requires a path")
			}
			explicit = args[i]
		case strings.HasPrefix(a, "--socket="):
			explicit = strings.TrimPrefix(a, "--socket=")
		case a == "-h" || a == "--help":
			return "", errHelp
		default:
			return "", fmt.Errorf("unknown flag %q", a)
		}
	}
	if explicit == "" {
		explicit = os.Getenv("OMLS_SOCKET")
	}
	sock, err := cluster.ResolveSocket(explicit)
	if err != nil {
		return "", err
	}
	return sock, nil
}

var errHelp = errors.New("help")

func helpRequested(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

func parseGraphArgs(args []string) (sock, out string, err error) {
	explicit := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--socket":
			i++
			if i >= len(args) {
				return "", "", fmt.Errorf("--socket requires a path")
			}
			explicit = args[i]
		case strings.HasPrefix(a, "--socket="):
			explicit = strings.TrimPrefix(a, "--socket=")
		case a == "--out" || a == "-o":
			i++
			if i >= len(args) {
				return "", "", fmt.Errorf("%s requires a path", a)
			}
			out = args[i]
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		case a == "-h" || a == "--help":
			return "", "", errHelp
		default:
			return "", "", fmt.Errorf("unknown flag %q", a)
		}
	}
	sock, err = cluster.ResolveSocket(explicit)
	if err != nil {
		return "", "", err
	}
	return sock, out, nil
}

func parseDemoArgs(args []string) (string, cluster.DemoRequest, error) {
	explicit := ""
	req := cluster.DemoRequest{
		Workers: 8, Iterations: 3, WorkIterations: 3_000_000, Policy: "adaptive", TimeoutMs: 180_000,
	}
	device := "cpu"
	var envFile, envPreset string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--socket":
			i++
			if i >= len(args) {
				return "", req, fmt.Errorf("--socket requires a path")
			}
			explicit = args[i]
		case strings.HasPrefix(a, "--socket="):
			explicit = strings.TrimPrefix(a, "--socket=")
		case a == "--workers":
			i++
			n, err := parseInt(need(args, &i, "--workers"))
			if err != nil {
				return "", req, err
			}
			req.Workers = int32(n)
		case strings.HasPrefix(a, "--workers="):
			n, err := parseInt(strings.TrimPrefix(a, "--workers="))
			if err != nil {
				return "", req, err
			}
			req.Workers = int32(n)
		case a == "--iterations":
			i++
			n, err := parseInt(need(args, &i, "--iterations"))
			if err != nil {
				return "", req, err
			}
			req.Iterations = int32(n)
		case strings.HasPrefix(a, "--iterations="):
			n, err := parseInt(strings.TrimPrefix(a, "--iterations="))
			if err != nil {
				return "", req, err
			}
			req.Iterations = int32(n)
		case a == "--work-iterations":
			i++
			n, err := parseInt64(need(args, &i, "--work-iterations"))
			if err != nil {
				return "", req, err
			}
			req.WorkIterations = n
		case strings.HasPrefix(a, "--work-iterations="):
			n, err := parseInt64(strings.TrimPrefix(a, "--work-iterations="))
			if err != nil {
				return "", req, err
			}
			req.WorkIterations = n
		case a == "--policy":
			i++
			req.Policy = need(args, &i, "--policy")
		case strings.HasPrefix(a, "--policy="):
			req.Policy = strings.TrimPrefix(a, "--policy=")
		case a == "--device":
			i++
			device = strings.ToLower(need(args, &i, "--device"))
		case strings.HasPrefix(a, "--device="):
			device = strings.ToLower(strings.TrimPrefix(a, "--device="))
		case a == "--preset":
			i++
			envPreset = need(args, &i, "--preset")
		case strings.HasPrefix(a, "--preset="):
			envPreset = strings.TrimPrefix(a, "--preset=")
		case a == "--envelope":
			i++
			envFile = need(args, &i, "--envelope")
		case strings.HasPrefix(a, "--envelope="):
			envFile = strings.TrimPrefix(a, "--envelope=")
		case a == "-h" || a == "--help":
			return "", req, errHelp
		default:
			return "", req, fmt.Errorf("unknown flag %q", a)
		}
	}
	if device != "cpu" && device != "gpu" {
		return "", req, fmt.Errorf("--device must be cpu or gpu")
	}
	if device == "gpu" && (envFile != "" || envPreset != "") {
		return "", req, fmt.Errorf("GPU mode does not support --preset or --envelope yet")
	}
	switch {
	case envFile != "":
		env, err := envelope.LoadFile(envFile)
		if err != nil {
			return "", req, err
		}
		raw, err := env.MarshalYAML()
		if err != nil {
			return "", req, err
		}
		req.EnvelopeYAML = raw
	case envPreset != "":
		env, err := envelope.Preset(envPreset)
		if err != nil {
			return "", req, err
		}
		raw, err := env.MarshalYAML()
		if err != nil {
			return "", req, err
		}
		req.EnvelopeYAML = raw
	}
	if device == "gpu" {
		req.Policy = "gpu-serial:" + req.Policy
	}
	sock, err := cluster.ResolveSocket(explicit)
	if err != nil {
		return "", req, err
	}
	return sock, req, nil
}

func need(args []string, i *int, flag string) string {
	if *i >= len(args) {
		return ""
	}
	return args[*i]
}

func daemonClientErr(err error) error {
	if errors.Is(err, errHelp) {
		return errHelp
	}
	if errors.Is(err, cluster.ErrStopped) || os.IsNotExist(err) {
		fmt.Println("OMLS: stopped")
		return errDaemonStopped
	}
	return err
}

func daemonUsage() string {
	return `omls daemon — resident cluster node (omlsd)

Usage:
  omls daemon [--data-dir PATH] [--listen :7443] [--socket PATH]
              [--heartbeat-timeout 15s] [--interval 5s] [--community-dir DIR]

The daemon discovers hardware, keeps a stable node id, and advertises
_omls._tcp.local. No master address is required. The CLI talks to this
process over a local socket.

omlsd is the same binary: install it as the daemon name if you prefer.
`
}

func clusterStatusUsage() string {
	return `omls status — local cluster summary

Usage:
  omls status [--socket PATH]
`
}

func clusterNodesUsage() string {
	return `omls nodes — cluster members

Usage:
  omls nodes [--socket PATH]
`
}

func clusterInfoUsage() string {
	return `omls cluster — coordinator and membership

Usage:
  omls cluster [--socket PATH]
`
}

func clusterGraphUsage() string {
	return `omls graph — Resource Graph from the local daemon

Usage:
  omls graph [--out graph.yaml] [--socket PATH]

The daemon asks the current coordinator. No master address is required.
`
}

func clusterDemoUsage() string {
	return `omls run-demo — schedule work across the discovered cluster

Usage:
  omls run-demo [--workers 8] [--iterations 3] [--work-iterations N]
                [--device cpu|gpu] [--policy adaptive|ewma]
                [--preset eco | --envelope FILE] [--socket PATH]

The local daemon forwards the job to the current coordinator.
`
}
