// Command omls is the Operational Machine Learning System CLI.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/discover"
	"github.com/Catatonic-Phobos/OMLS/internal/fabric"
	"github.com/Catatonic-Phobos/OMLS/internal/fabric/tlsconfig"
	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
	omlsv1 "github.com/Catatonic-Phobos/OMLS/proto/omls/v1"
	"google.golang.org/grpc/credentials"
)

const version = "0.2.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "agent":
		err = runAgent(os.Args[2:])
	case "master":
		err = runMaster(os.Args[2:])
	case "version", "--version", "-V":
		fmt.Printf("omls %s\n", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "omls: %v\n", err)
		os.Exit(1)
	}
}

func runAgent(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected subcommand (discover|run|register)")
	}
	switch args[0] {
	case "discover":
		return runDiscover(args[1:])
	case "run":
		return runAgentLoop(args[1:], false)
	case "register":
		return runAgentLoop(args[1:], true)
	case "help", "-h", "--help":
		fmt.Print(agentUsage())
		return nil
	default:
		return fmt.Errorf("unknown agent subcommand %q", args[0])
	}
}

func runMaster(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected subcommand (serve|graph|health)")
	}
	switch args[0] {
	case "serve":
		return runMasterServe(args[1:])
	case "graph":
		return runMasterGraph(args[1:])
	case "health":
		return runMasterHealth(args[1:])
	case "help", "-h", "--help":
		fmt.Print(masterUsage())
		return nil
	default:
		return fmt.Errorf("unknown master subcommand %q", args[0])
	}
}

func runDiscover(args []string) error {
	out := "machine-profile.yaml"
	format := ""
	quiet := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--out" || a == "-o":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a path", a)
			}
			i++
			out = args[i]
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		case a == "--format":
			if i+1 >= len(args) {
				return fmt.Errorf("--format requires yaml|json")
			}
			i++
			format = args[i]
		case strings.HasPrefix(a, "--format="):
			format = strings.TrimPrefix(a, "--format=")
		case a == "--quiet" || a == "-q":
			quiet = true
		case a == "-h" || a == "--help":
			fmt.Print(discoverUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}

	if format == "" {
		format = rdl.FormatFromPath(out)
	}

	res, err := discover.Discover(discover.Options{})
	if err != nil {
		return err
	}
	if err := rdl.WriteFile(out, format, &res.Document); err != nil {
		return err
	}

	if !quiet {
		for _, w := range res.Warnings {
			fmt.Fprintf(os.Stderr, "warning: %s\n", w)
		}
		fmt.Fprintf(os.Stderr, "wrote %s (node=%s virt=%s resources=%d transports=%d)\n",
			out, res.Document.Node.ID, res.Document.Node.Virt,
			len(res.Document.Resources), len(res.Document.Transports))
	}
	return nil
}

type tlsFlags struct {
	ca, cert, key, serverName string
	insecure                  bool
}

func parseTLSFlags(args []string) (tlsFlags, []string, error) {
	var f tlsFlags
	rest := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--ca":
			i++
			if i >= len(args) {
				return f, nil, fmt.Errorf("--ca requires a path")
			}
			f.ca = args[i]
		case strings.HasPrefix(a, "--ca="):
			f.ca = strings.TrimPrefix(a, "--ca=")
		case a == "--cert":
			i++
			if i >= len(args) {
				return f, nil, fmt.Errorf("--cert requires a path")
			}
			f.cert = args[i]
		case strings.HasPrefix(a, "--cert="):
			f.cert = strings.TrimPrefix(a, "--cert=")
		case a == "--key":
			i++
			if i >= len(args) {
				return f, nil, fmt.Errorf("--key requires a path")
			}
			f.key = args[i]
		case strings.HasPrefix(a, "--key="):
			f.key = strings.TrimPrefix(a, "--key=")
		case a == "--server-name":
			i++
			if i >= len(args) {
				return f, nil, fmt.Errorf("--server-name requires a value")
			}
			f.serverName = args[i]
		case strings.HasPrefix(a, "--server-name="):
			f.serverName = strings.TrimPrefix(a, "--server-name=")
		case a == "--insecure":
			f.insecure = true
		default:
			rest = append(rest, a)
		}
	}
	return f, rest, nil
}

func (f tlsFlags) clientCreds() (credentials.TransportCredentials, error) {
	if f.insecure {
		return nil, nil
	}
	if f.ca == "" || f.cert == "" || f.key == "" {
		return nil, fmt.Errorf("mTLS requires --ca --cert --key (or pass --insecure for local lab only)")
	}
	cfg, err := tlsconfig.LoadClient(f.ca, f.cert, f.key, f.serverName)
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(cfg), nil
}

func (f tlsFlags) serverCreds() (credentials.TransportCredentials, error) {
	if f.insecure {
		return nil, nil
	}
	if f.ca == "" || f.cert == "" || f.key == "" {
		return nil, fmt.Errorf("mTLS requires --ca --cert --key (or pass --insecure for local lab only)")
	}
	cfg, err := tlsconfig.LoadServer(f.ca, f.cert, f.key)
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(cfg), nil
}

func runAgentLoop(args []string, once bool) error {
	tlsF, rest, err := parseTLSFlags(args)
	if err != nil {
		return err
	}
	master := "127.0.0.1:7443"
	interval := 5 * time.Second
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--master":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--master requires host:port")
			}
			master = rest[i]
		case strings.HasPrefix(a, "--master="):
			master = strings.TrimPrefix(a, "--master=")
		case a == "--interval":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--interval requires a duration")
			}
			interval, err = time.ParseDuration(rest[i])
			if err != nil {
				return err
			}
		case strings.HasPrefix(a, "--interval="):
			interval, err = time.ParseDuration(strings.TrimPrefix(a, "--interval="))
			if err != nil {
				return err
			}
		case a == "-h" || a == "--help":
			fmt.Print(agentRunUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}

	creds, err := tlsF.clientCreds()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, client, err := fabric.Dial(ctx, fabric.DialOptions{
		Addr:    master,
		TLS:     creds,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "omls agent: connected to %s (once=%v insecure=%v)\n", master, once, tlsF.insecure)
	return fabric.Run(ctx, fabric.AgentConfig{
		Client:       client,
		Conn:         conn,
		Interval:     interval,
		AgentVersion: version,
		Once:         once,
	})
}

func runMasterServe(args []string) error {
	tlsF, rest, err := parseTLSFlags(args)
	if err != nil {
		return err
	}
	listen := ":7443"
	hbTimeout := 15 * time.Second
	interval := 5 * time.Second
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--listen":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--listen requires addr")
			}
			listen = rest[i]
		case strings.HasPrefix(a, "--listen="):
			listen = strings.TrimPrefix(a, "--listen=")
		case a == "--heartbeat-timeout":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--heartbeat-timeout requires duration")
			}
			hbTimeout, err = time.ParseDuration(rest[i])
			if err != nil {
				return err
			}
		case strings.HasPrefix(a, "--heartbeat-timeout="):
			hbTimeout, err = time.ParseDuration(strings.TrimPrefix(a, "--heartbeat-timeout="))
			if err != nil {
				return err
			}
		case a == "--interval":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--interval requires duration")
			}
			interval, err = time.ParseDuration(rest[i])
			if err != nil {
				return err
			}
		case strings.HasPrefix(a, "--interval="):
			interval, err = time.ParseDuration(strings.TrimPrefix(a, "--interval="))
			if err != nil {
				return err
			}
		case a == "-h" || a == "--help":
			fmt.Print(masterServeUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}

	creds, err := tlsF.serverCreds()
	if err != nil {
		return err
	}
	reg := graph.New(hbTimeout)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(os.Stderr, "omls master: listening on %s (insecure=%v heartbeat-timeout=%s)\n",
		listen, tlsF.insecure, hbTimeout)
	return fabric.ListenAndServe(ctx, fabric.ServeOptions{
		Listen:   listen,
		TLS:      creds,
		Registry: reg,
		Interval: interval,
	})
}

func runMasterGraph(args []string) error {
	tlsF, rest, err := parseTLSFlags(args)
	if err != nil {
		return err
	}
	master := "127.0.0.1:7443"
	out := "graph.yaml"
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--master":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--master requires host:port")
			}
			master = rest[i]
		case strings.HasPrefix(a, "--master="):
			master = strings.TrimPrefix(a, "--master=")
		case a == "--out" || a == "-o":
			i++
			if i >= len(rest) {
				return fmt.Errorf("%s requires a path", a)
			}
			out = rest[i]
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		case a == "-h" || a == "--help":
			fmt.Print(masterGraphUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	creds, err := tlsF.clientCreds()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, client, err := fabric.Dial(ctx, fabric.DialOptions{Addr: master, TLS: creds, Timeout: 5 * time.Second})
	if err != nil {
		return err
	}
	defer conn.Close()
	resp, err := client.GetGraph(ctx, &omlsv1.GetGraphRequest{})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil && filepath.Dir(out) != "." {
		return err
	}
	if err := os.WriteFile(out, resp.GetGraphYaml(), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", out, len(resp.GetGraphYaml()))
	return nil
}

func runMasterHealth(args []string) error {
	tlsF, rest, err := parseTLSFlags(args)
	if err != nil {
		return err
	}
	master := "127.0.0.1:7443"
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--master":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--master requires host:port")
			}
			master = rest[i]
		case strings.HasPrefix(a, "--master="):
			master = strings.TrimPrefix(a, "--master=")
		case a == "-h" || a == "--help":
			fmt.Print(masterHealthUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	creds, err := tlsF.clientCreds()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, client, err := fabric.Dial(ctx, fabric.DialOptions{Addr: master, TLS: creds, Timeout: 5 * time.Second})
	if err != nil {
		return err
	}
	defer conn.Close()
	h, err := client.Health(ctx, &omlsv1.HealthRequest{})
	if err != nil {
		return err
	}
	fmt.Printf("status=%s version=%s nodes=%d available=%d\n",
		h.GetStatus(), h.GetVersion(), h.GetNodeCount(), h.GetAvailableNodes())
	return nil
}

func usage() {
	fmt.Print(`omls — Operational Machine Learning System

Usage:
  omls agent discover [--out machine-profile.yaml] [--format yaml|json]
  omls agent run --master HOST:PORT [--ca --cert --key | --insecure]
  omls agent register --master HOST:PORT [...]   # one-shot join
  omls master serve [--listen :7443] [--ca --cert --key | --insecure]
  omls master graph --master HOST:PORT --out graph.yaml
  omls master health --master HOST:PORT
  omls version

OMLS 0.2: local discovery + multi-node fabric (gRPC, mTLS).
`)
}

func agentUsage() string {
	return `omls agent — local node agent

Usage:
  omls agent discover [flags]
  omls agent run [flags]
  omls agent register [flags]

Commands:
  discover   Probe sysfs/proc and write an RDL Machine Profile
  run        Discover, register, advertise, and heartbeat to a master
  register   One-shot register + advertise + heartbeat
`
}

func agentRunUsage() string {
	return `omls agent run — join the fabric

Usage:
  omls agent run --master HOST:PORT [--interval 5s] (--ca CA --cert CERT --key KEY | --insecure)

Flags:
  --master HOST:PORT   Master address (default 127.0.0.1:7443)
  --interval DUR       Heartbeat interval (default 5s, or master-suggested)
  --ca/--cert/--key    mTLS materials
  --server-name NAME   TLS ServerName (default localhost)
  --insecure           Dev-only: plaintext gRPC (lab/localhost)
`
}

func discoverUsage() string {
	return `omls agent discover — write a Machine Profile (RDL v0)

Usage:
  omls agent discover [--out PATH] [--format yaml|json] [--quiet]

Flags:
  --out, -o PATH     Output path (default: machine-profile.yaml)
  --format FORMAT    yaml or json (default: from --out suffix)
  --quiet, -q        Suppress warnings on stderr
`
}

func masterUsage() string {
	return `omls master — Resource Graph coordinator

Usage:
  omls master serve [flags]
  omls master graph [flags]
  omls master health [flags]
`
}

func masterServeUsage() string {
	return `omls master serve — run the fabric control plane

Usage:
  omls master serve [--listen :7443] [--heartbeat-timeout 15s] (--ca CA --cert CERT --key KEY | --insecure)

Flags:
  --listen ADDR              Bind address (default :7443)
  --heartbeat-timeout DUR    Mark node unavailable after this silence (default 15s)
  --interval DUR             Suggested agent heartbeat interval (default 5s)
  --ca/--cert/--key          mTLS materials (server requires client certs)
  --insecure                 Dev-only plaintext gRPC
`
}

func masterGraphUsage() string {
	return `omls master graph — dump the Resource Graph

Usage:
  omls master graph --master HOST:PORT --out graph.yaml (--ca --cert --key | --insecure)
`
}

func masterHealthUsage() string {
	return `omls master health — probe master liveness

Usage:
  omls master health --master HOST:PORT (--ca --cert --key | --insecure)
`
}
