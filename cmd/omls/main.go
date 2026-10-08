// Command omls is the Operational Machine Learning System CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/community"
	"github.com/Catatonic-Phobos/OMLS/internal/discover"
	"github.com/Catatonic-Phobos/OMLS/internal/envelope"
	"github.com/Catatonic-Phobos/OMLS/internal/fabric"
	"github.com/Catatonic-Phobos/OMLS/internal/fabric/tlsconfig"
	"github.com/Catatonic-Phobos/OMLS/internal/gpu"
	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/power"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
	"github.com/Catatonic-Phobos/OMLS/internal/sandbox"
	omlsv1 "github.com/Catatonic-Phobos/OMLS/proto/omls/v1"
	"google.golang.org/grpc/credentials"
	"gopkg.in/yaml.v3"
)

const version = "1.1.0"

func main() {
	if filepath.Base(os.Args[0]) == "omlsd" {
		err := runDaemon(os.Args[1:])
		exitErr(err)
		return
	}
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
	case "daemon":
		err = runDaemon(os.Args[2:])
	case "status":
		err = runClusterStatus(os.Args[2:])
	case "nodes":
		err = runClusterNodes(os.Args[2:])
	case "cluster":
		err = runClusterInfo(os.Args[2:])
	case "graph":
		err = runClusterGraph(os.Args[2:])
	case "run-demo":
		err = runClusterDemo(os.Args[2:])
	case "community":
		err = runCommunity(os.Args[2:])
	case "power":
		err = runPower(os.Args[2:])
	case "update":
		err = runUpdate(os.Args[2:])
	case "version", "--version", "-V":
		fmt.Printf("omls %s\n", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	exitErr(err)
}

func exitErr(err error) {
	if err == nil || errors.Is(err, errHelp) {
		return
	}
	if errors.Is(err, errDaemonStopped) {
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "omls: %v\n", err)
	os.Exit(1)
}

func runAgent(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected subcommand (discover|run|register|envelope|sandbox)")
	}
	switch args[0] {
	case "discover":
		return runDiscover(args[1:])
	case "run":
		return runAgentLoop(args[1:], false)
	case "register":
		return runAgentLoop(args[1:], true)
	case "envelope":
		return runEnvelope(args[1:])
	case "sandbox":
		return runSandbox(args[1:])
	case "help", "-h", "--help":
		fmt.Print(agentUsage())
		return nil
	default:
		return fmt.Errorf("unknown agent subcommand %q", args[0])
	}
}

func runMaster(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected subcommand (serve|graph|health|profiles|run-demo)")
	}
	switch args[0] {
	case "serve":
		return runMasterServe(args[1:])
	case "graph":
		return runMasterGraph(args[1:])
	case "health":
		return runMasterHealth(args[1:])
	case "profiles":
		return runMasterProfiles(args[1:])
	case "run-demo":
		return runMasterDemo(args[1:])
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
	withSandbox := false

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
		case a == "--sandbox":
			withSandbox = true
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
	if err := gpu.PrepareOpenCL(); err != nil {
		return err
	}

	res, err := discover.Discover(discover.Options{})
	if err != nil {
		return err
	}
	if withSandbox {
		probe := sandbox.Probe(sandbox.Options{})
		sandbox.MergeIntoDocument(&res.Document, probe)
		res.Warnings = append(res.Warnings, probe.Warnings...)
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

func runSandbox(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected sandbox subcommand (probe|list|claim)")
	}
	switch args[0] {
	case "probe":
		return sandboxProbe(args[1:])
	case "list":
		return sandboxList(args[1:])
	case "claim":
		return sandboxClaim(args[1:])
	case "help", "-h", "--help":
		fmt.Print(sandboxUsage())
		return nil
	default:
		return fmt.Errorf("unknown sandbox subcommand %q", args[0])
	}
}

func sandboxProbe(args []string) error {
	sysRoot := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--sys-root":
			i++
			if i >= len(args) {
				return fmt.Errorf("--sys-root requires a path")
			}
			sysRoot = args[i]
		case strings.HasPrefix(a, "--sys-root="):
			sysRoot = strings.TrimPrefix(a, "--sys-root=")
		case a == "-h" || a == "--help":
			fmt.Print(sandboxUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	res := sandbox.Probe(sandbox.Options{SysRoot: sysRoot})
	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	fmt.Printf("backend=%s vfio=%v uio=%v devices=%d\n",
		res.Backend, res.VFIOPresent, res.UIOPresent, len(res.Devices))
	raw, err := yaml.Marshal(res)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(raw)
	return err
}

func sandboxList(args []string) error {
	sysRoot := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--sys-root":
			i++
			if i >= len(args) {
				return fmt.Errorf("--sys-root requires a path")
			}
			sysRoot = args[i]
		case strings.HasPrefix(a, "--sys-root="):
			sysRoot = strings.TrimPrefix(a, "--sys-root=")
		case a == "-h" || a == "--help":
			fmt.Print(sandboxUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	res := sandbox.Probe(sandbox.Options{SysRoot: sysRoot})
	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	if len(res.Devices) == 0 {
		fmt.Println("(no sandbox devices; backend=" + string(res.Backend) + ")")
		return nil
	}
	fmt.Printf("%-20s %-10s %-8s %s\n", "ID", "BACKEND", "CLAIM", "NAME")
	for _, d := range res.Devices {
		fmt.Printf("%-20s %-10s %-8v %s\n", d.ID, d.Backend, d.Claimable, d.Name)
	}
	return nil
}

func sandboxClaim(args []string) error {
	sysRoot := ""
	id := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--sys-root":
			i++
			if i >= len(args) {
				return fmt.Errorf("--sys-root requires a path")
			}
			sysRoot = args[i]
		case strings.HasPrefix(a, "--sys-root="):
			sysRoot = strings.TrimPrefix(a, "--sys-root=")
		case a == "--id":
			i++
			if i >= len(args) {
				return fmt.Errorf("--id requires a device id")
			}
			id = args[i]
		case strings.HasPrefix(a, "--id="):
			id = strings.TrimPrefix(a, "--id=")
		case a == "-h" || a == "--help":
			fmt.Print(sandboxUsage())
			return nil
		default:
			if !strings.HasPrefix(a, "-") && id == "" {
				id = a
				continue
			}
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	if id == "" {
		return fmt.Errorf("device id required")
	}
	res := sandbox.Claim(sandbox.Options{SysRoot: sysRoot}, id)
	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	fmt.Printf("ok=%v simulated=%v backend=%s device=%s\n%s\n",
		res.OK, res.Simulated, res.Backend, res.DeviceID, res.Message)
	if !res.OK {
		return fmt.Errorf("claim failed")
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
	if err := gpu.PrepareOpenCL(); err != nil {
		return err
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
	dataDir := "omls-data"
	communityDir := ""
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
		case a == "--data-dir":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--data-dir requires a path")
			}
			dataDir = rest[i]
		case strings.HasPrefix(a, "--data-dir="):
			dataDir = strings.TrimPrefix(a, "--data-dir=")
		case a == "--community-dir":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--community-dir requires a path")
			}
			communityDir = rest[i]
		case strings.HasPrefix(a, "--community-dir="):
			communityDir = strings.TrimPrefix(a, "--community-dir=")
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
	if communityDir == "" {
		communityDir = community.ResolveDir("")
	}
	fmt.Fprintf(os.Stderr, "omls master: listening on %s (insecure=%v heartbeat-timeout=%s data-dir=%s community-dir=%s)\n",
		listen, tlsF.insecure, hbTimeout, dataDir, communityDir)
	return fabric.ListenAndServe(ctx, fabric.ServeOptions{
		Listen:       listen,
		TLS:          creds,
		Registry:     reg,
		Interval:     interval,
		DataDir:      dataDir,
		CommunityDir: communityDir,
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

func runEnvelope(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected envelope subcommand (validate|show|apply)")
	}
	switch args[0] {
	case "validate":
		return envelopeValidate(args[1:])
	case "show":
		return envelopeShow(args[1:])
	case "apply":
		return envelopeApply(args[1:])
	case "help", "-h", "--help":
		fmt.Print(envelopeUsage())
		return nil
	default:
		return fmt.Errorf("unknown envelope subcommand %q", args[0])
	}
}

func loadEnvelopeFromFlags(args []string) (envelope.Envelope, []string, error) {
	var file, preset string
	rest := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--file" || a == "-f":
			i++
			if i >= len(args) {
				return envelope.Envelope{}, nil, fmt.Errorf("%s requires a path", a)
			}
			file = args[i]
		case strings.HasPrefix(a, "--file="):
			file = strings.TrimPrefix(a, "--file=")
		case a == "--preset":
			i++
			if i >= len(args) {
				return envelope.Envelope{}, nil, fmt.Errorf("--preset requires a name")
			}
			preset = args[i]
		case strings.HasPrefix(a, "--preset="):
			preset = strings.TrimPrefix(a, "--preset=")
		default:
			rest = append(rest, a)
		}
	}
	switch {
	case file != "":
		env, err := envelope.LoadFile(file)
		return env, rest, err
	case preset != "":
		env, err := envelope.Preset(preset)
		return env, rest, err
	default:
		return envelope.Default(), rest, nil
	}
}

func envelopeValidate(args []string) error {
	env, rest, err := loadEnvelopeFromFlags(args)
	if err != nil {
		return err
	}
	for _, a := range rest {
		if a == "-h" || a == "--help" {
			fmt.Print(envelopeUsage())
			return nil
		}
		return fmt.Errorf("unknown flag %q", a)
	}
	fmt.Printf("ok envelope=%q performance=%s attack=%.2f peak=%.2f sustain=%.2f release=%s/%s thermal_max_c=%.0f\n",
		env.Name, env.Performance, env.Attack.Level, env.Peak.Level, env.Sustain.Level,
		env.Release.Mode, env.Release.Duration.Std(), env.Limits.ThermalMaxC)
	return nil
}

func envelopeShow(args []string) error {
	env, rest, err := loadEnvelopeFromFlags(args)
	if err != nil {
		return err
	}
	for _, a := range rest {
		if a == "-h" || a == "--help" {
			fmt.Print(envelopeUsage())
			return nil
		}
		return fmt.Errorf("unknown flag %q", a)
	}
	raw, err := env.MarshalYAML()
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(raw)
	return err
}

func envelopeApply(args []string) error {
	env, rest, err := loadEnvelopeFromFlags(args)
	if err != nil {
		return err
	}
	dur := 2 * time.Second
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--duration":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--duration requires a value")
			}
			dur, err = time.ParseDuration(rest[i])
			if err != nil {
				return err
			}
		case strings.HasPrefix(a, "--duration="):
			dur, err = time.ParseDuration(strings.TrimPrefix(a, "--duration="))
			if err != nil {
				return err
			}
		case a == "-h" || a == "--help":
			fmt.Print(envelopeUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	backends, warns := envelope.OpenBackends()
	for _, w := range warns {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	ctx, cancel := context.WithTimeout(context.Background(), dur+env.Attack.Duration.Std()+env.Peak.Duration.Std()+env.Release.Duration.Std()+time.Second)
	defer cancel()
	// Hold sustain for --duration.
	env.Sustain.Duration = envelope.Duration(dur)
	ctrl := &envelope.Controller{Env: env, Backends: backends}
	rep, err := ctrl.Apply(ctx)
	if err != nil && ctx.Err() == nil {
		return err
	}
	fmt.Printf("applied envelope=%q backends=%v phases=%v final_level=%.2f\n",
		rep.Envelope, rep.Backends, rep.Phases, rep.FinalLevel)
	for _, w := range rep.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	return nil
}

func runMasterDemo(args []string) error {
	tlsF, rest, err := parseTLSFlags(args)
	if err != nil {
		return err
	}
	master := "127.0.0.1:7443"
	workers := int32(8)
	iterations := int32(3)
	workIters := int64(3_000_000)
	policy := "ewma"
	device := "cpu"
	var envFile, envPreset string
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--device":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--device requires cpu|gpu")
			}
			device = strings.ToLower(rest[i])
		case strings.HasPrefix(a, "--device="):
			device = strings.ToLower(strings.TrimPrefix(a, "--device="))
		case a == "--policy":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--policy requires ewma|adaptive")
			}
			policy = rest[i]
		case strings.HasPrefix(a, "--policy="):
			policy = strings.TrimPrefix(a, "--policy=")
		case a == "--master":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--master requires host:port")
			}
			master = rest[i]
		case strings.HasPrefix(a, "--master="):
			master = strings.TrimPrefix(a, "--master=")
		case a == "--workers":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--workers requires an int")
			}
			var n int
			n, err = parseInt(rest[i])
			if err != nil {
				return err
			}
			workers = int32(n)
		case strings.HasPrefix(a, "--workers="):
			var n int
			n, err = parseInt(strings.TrimPrefix(a, "--workers="))
			if err != nil {
				return err
			}
			workers = int32(n)
		case a == "--iterations":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--iterations requires an int")
			}
			var n int
			n, err = parseInt(rest[i])
			if err != nil {
				return err
			}
			iterations = int32(n)
		case strings.HasPrefix(a, "--iterations="):
			var n int
			n, err = parseInt(strings.TrimPrefix(a, "--iterations="))
			if err != nil {
				return err
			}
			iterations = int32(n)
		case a == "--work-iterations":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--work-iterations requires an int")
			}
			workIters, err = parseInt64(rest[i])
			if err != nil {
				return err
			}
		case strings.HasPrefix(a, "--work-iterations="):
			workIters, err = parseInt64(strings.TrimPrefix(a, "--work-iterations="))
			if err != nil {
				return err
			}
		case a == "--envelope":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--envelope requires a path")
			}
			envFile = rest[i]
		case strings.HasPrefix(a, "--envelope="):
			envFile = strings.TrimPrefix(a, "--envelope=")
		case a == "--preset":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--preset requires a name")
			}
			envPreset = rest[i]
		case strings.HasPrefix(a, "--preset="):
			envPreset = strings.TrimPrefix(a, "--preset=")
		case a == "-h" || a == "--help":
			fmt.Print(masterDemoUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	if device != "cpu" && device != "gpu" {
		return fmt.Errorf("--device must be cpu or gpu")
	}
	if device == "gpu" && (envFile != "" || envPreset != "") {
		return fmt.Errorf("GPU mode does not support --preset or --envelope yet")
	}
	var envYAML []byte
	switch {
	case envFile != "":
		env, err := envelope.LoadFile(envFile)
		if err != nil {
			return err
		}
		envYAML, err = env.MarshalYAML()
		if err != nil {
			return err
		}
	case envPreset != "":
		env, err := envelope.Preset(envPreset)
		if err != nil {
			return err
		}
		envYAML, err = env.MarshalYAML()
		if err != nil {
			return err
		}
	}
	creds, err := tlsF.clientCreds()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	conn, client, err := fabric.Dial(ctx, fabric.DialOptions{Addr: master, TLS: creds, Timeout: 5 * time.Second})
	if err != nil {
		return err
	}
	defer conn.Close()

	rpcPolicy := policy
	if device == "gpu" {
		rpcPolicy = "gpu-serial:" + policy
	}
	fmt.Fprintf(os.Stderr, "omls master run-demo: device=%s workers=%d iterations=%d work_iterations=%d envelope=%v policy=%s\n",
		device, workers, iterations, workIters, envFile != "" || envPreset != "", policy)
	resp, err := client.RunDemo(ctx, &omlsv1.RunDemoRequest{
		Workers:        workers,
		Iterations:     iterations,
		WorkIterations: workIters,
		TimeoutMs:      180_000,
		EnvelopeYaml:   envYAML,
		Policy:         rpcPolicy,
	})
	if err != nil {
		return err
	}
	for _, r := range resp.GetRounds() {
		fmt.Printf("\n=== round %d (%d ms) ===\n", r.GetRound(), r.GetWallMs())
		fmt.Printf("policy: %s\n", r.GetPolicyNote())
		for _, a := range r.GetAllocations() {
			fmt.Printf("  %-12s workers=%d score=%.1f virt=%s host=%s\n",
				short(a.GetNodeId()), a.GetWorkers(), a.GetScore(), a.GetVirt(), a.GetHostname())
			if a.GetReason() != "" {
				fmt.Printf("             reason: %s\n", a.GetReason())
			}
		}
		if len(r.GetDurationEwmaMs()) > 0 {
			fmt.Printf("  duration_ewma_ms: %v\n", r.GetDurationEwmaMs())
		}
	}
	fmt.Printf("\nsummary: %s\n", resp.GetSummary())
	return nil
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return 0, fmt.Errorf("invalid int %q", s)
	}
	return n, nil
}

func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return 0, fmt.Errorf("invalid int %q", s)
	}
	return n, nil
}

func short(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func runMasterProfiles(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected profiles subcommand (list|show)")
	}
	switch args[0] {
	case "list":
		return runMasterProfilesList(args[1:])
	case "show":
		return runMasterProfilesShow(args[1:])
	case "help", "-h", "--help":
		fmt.Print(masterProfilesUsage())
		return nil
	default:
		return fmt.Errorf("unknown profiles subcommand %q", args[0])
	}
}

func runMasterProfilesList(args []string) error {
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
			fmt.Print(masterProfilesUsage())
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
	resp, err := client.ListProfiles(ctx, &omlsv1.ListProfilesRequest{})
	if err != nil {
		return err
	}
	if resp.GetDataDir() != "" {
		fmt.Fprintf(os.Stderr, "data_dir=%s profiles=%d\n", resp.GetDataDir(), len(resp.GetProfiles()))
	}
	if len(resp.GetProfiles()) == 0 {
		fmt.Println("(no behavior profiles yet)")
		return nil
	}
	fmt.Printf("%-36s %-16s %-8s %8s %12s %10s %s\n",
		"NODE_ID", "HOSTNAME", "VIRT", "SAMPLES", "EWMA_MS", "CONF", "UPDATED")
	for _, p := range resp.GetProfiles() {
		fmt.Printf("%-36s %-16s %-8s %8d %12.1f %10.3f %s\n",
			p.GetNodeId(), truncate(p.GetHostname(), 16), p.GetVirt(),
			p.GetSamples(), p.GetDurationEwmaMs(), p.GetConfidence(), p.GetUpdatedAt())
	}
	return nil
}

func runMasterProfilesShow(args []string) error {
	tlsF, rest, err := parseTLSFlags(args)
	if err != nil {
		return err
	}
	master := "127.0.0.1:7443"
	nodeID := ""
	out := ""
	telOut := ""
	tail := int32(20)
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
		case a == "--node":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--node requires a node id")
			}
			nodeID = rest[i]
		case strings.HasPrefix(a, "--node="):
			nodeID = strings.TrimPrefix(a, "--node=")
		case a == "--out" || a == "-o":
			i++
			if i >= len(rest) {
				return fmt.Errorf("%s requires a path", a)
			}
			out = rest[i]
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		case a == "--telemetry-out":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--telemetry-out requires a path")
			}
			telOut = rest[i]
		case strings.HasPrefix(a, "--telemetry-out="):
			telOut = strings.TrimPrefix(a, "--telemetry-out=")
		case a == "--telemetry-tail":
			i++
			if i >= len(rest) {
				return fmt.Errorf("--telemetry-tail requires an int")
			}
			n, err := parseInt(rest[i])
			if err != nil {
				return err
			}
			tail = int32(n)
		case strings.HasPrefix(a, "--telemetry-tail="):
			n, err := parseInt(strings.TrimPrefix(a, "--telemetry-tail="))
			if err != nil {
				return err
			}
			tail = int32(n)
		case a == "-h" || a == "--help":
			fmt.Print(masterProfilesUsage())
			return nil
		default:
			if nodeID == "" && !strings.HasPrefix(a, "-") {
				nodeID = a
				continue
			}
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	if nodeID == "" {
		return fmt.Errorf("--node is required")
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
	resp, err := client.GetProfile(ctx, &omlsv1.GetProfileRequest{NodeId: nodeID, TelemetryTail: tail})
	if err != nil {
		return err
	}
	if out != "" {
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil && filepath.Dir(out) != "." {
			return err
		}
		if err := os.WriteFile(out, resp.GetProfileYaml(), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", out, len(resp.GetProfileYaml()))
	} else {
		if _, err := os.Stdout.Write(resp.GetProfileYaml()); err != nil {
			return err
		}
	}
	if len(resp.GetTelemetryJsonl()) > 0 {
		if telOut != "" {
			if err := os.MkdirAll(filepath.Dir(telOut), 0o755); err != nil && filepath.Dir(telOut) != "." {
				return err
			}
			if err := os.WriteFile(telOut, resp.GetTelemetryJsonl(), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote telemetry %s (%d bytes)\n", telOut, len(resp.GetTelemetryJsonl()))
		} else if out == "" {
			fmt.Fprintln(os.Stderr, "--- telemetry (jsonl) ---")
			if _, err := os.Stdout.Write(resp.GetTelemetryJsonl()); err != nil {
				return err
			}
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func runCommunity(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected community subcommand (list|show|import)")
	}
	switch args[0] {
	case "list":
		return communityList(args[1:])
	case "show":
		return communityShow(args[1:])
	case "import":
		return communityImport(args[1:])
	case "help", "-h", "--help":
		fmt.Print(communityUsage())
		return nil
	default:
		return fmt.Errorf("unknown community subcommand %q", args[0])
	}
}

func parseCommunityDir(args []string) (dir string, rest []string, err error) {
	dir = ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--dir":
			i++
			if i >= len(args) {
				return "", nil, fmt.Errorf("--dir requires a path")
			}
			dir = args[i]
		case strings.HasPrefix(a, "--dir="):
			dir = strings.TrimPrefix(a, "--dir=")
		default:
			rest = append(rest, a)
		}
	}
	if dir == "" {
		dir = community.ResolveDir("")
	}
	return dir, rest, nil
}

func communityList(args []string) error {
	dir, rest, err := parseCommunityDir(args)
	if err != nil {
		return err
	}
	for _, a := range rest {
		if a == "-h" || a == "--help" {
			fmt.Print(communityUsage())
			return nil
		}
		return fmt.Errorf("unknown flag %q", a)
	}
	cat, err := community.Open(dir)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "community_dir=%s profiles=%d\n", cat.Dir, len(cat.Profiles))
	if len(cat.Profiles) == 0 {
		fmt.Println("(no community profiles)")
		return nil
	}
	fmt.Printf("%-24s %-12s %6s  %s\n", "ID", "CONFIDENCE", "SCORE", "TITLE")
	for _, p := range cat.Profiles {
		fmt.Printf("%-24s %-12s %6.2f  %s\n",
			p.ID, p.Provenance.Confidence, p.Provenance.ConfidenceScore, p.Title)
	}
	return nil
}

func communityShow(args []string) error {
	dir, rest, err := parseCommunityDir(args)
	if err != nil {
		return err
	}
	id := ""
	for _, a := range rest {
		if a == "-h" || a == "--help" {
			fmt.Print(communityUsage())
			return nil
		}
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag %q", a)
		}
		if id == "" {
			id = a
			continue
		}
		return fmt.Errorf("unexpected arg %q", a)
	}
	if id == "" {
		return fmt.Errorf("profile id required")
	}
	cat, err := community.Open(dir)
	if err != nil {
		return err
	}
	p, ok := cat.Get(id)
	if !ok {
		return fmt.Errorf("profile %q not found in %s", id, dir)
	}
	p.SourcePath = ""
	raw, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(raw)
	return err
}

func communityImport(args []string) error {
	dir, rest, err := parseCommunityDir(args)
	if err != nil {
		return err
	}
	src := ""
	for _, a := range rest {
		if a == "-h" || a == "--help" {
			fmt.Print(communityUsage())
			return nil
		}
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag %q", a)
		}
		if src == "" {
			src = a
			continue
		}
		return fmt.Errorf("unexpected arg %q", a)
	}
	if src == "" {
		return fmt.Errorf("path to profile yaml required")
	}
	p, err := community.ImportFile(dir, src)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "imported %s → %s\n", p.ID, p.SourcePath)
	return nil
}

func runPower(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected power subcommand (show|hello|budget)")
	}
	switch args[0] {
	case "show":
		return powerShow(args[1:])
	case "hello":
		return powerHello(args[1:])
	case "budget":
		return powerBudget(args[1:])
	case "help", "-h", "--help":
		fmt.Print(powerUsage())
		return nil
	default:
		return fmt.Errorf("unknown power subcommand %q", args[0])
	}
}

func powerShow(args []string) error {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(powerUsage())
			return nil
		}
		return fmt.Errorf("unknown flag %q", a)
	}
	sim := power.NewSimulator()
	st := sim.Handle(power.Message{Type: power.MsgStatus, SchemaVersion: power.SchemaVersion}).State
	if st == nil {
		return fmt.Errorf("no state")
	}
	for _, w := range st.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	raw, err := power.MarshalStateYAML(*st)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(raw)
	return err
}

func powerHello(args []string) error {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(powerUsage())
			return nil
		}
		return fmt.Errorf("unknown flag %q", a)
	}
	sim := power.NewSimulator()
	resp := sim.Handle(power.Message{Type: power.MsgHello, SchemaVersion: power.SchemaVersion})
	raw, err := yaml.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(raw)
	return err
}

func powerBudget(args []string) error {
	consumer := "cli"
	rail := "rail-12v"
	watts := 40.0
	envelopeFile := ""
	envelopePreset := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--consumer":
			i++
			if i >= len(args) {
				return fmt.Errorf("--consumer requires id")
			}
			consumer = args[i]
		case strings.HasPrefix(a, "--consumer="):
			consumer = strings.TrimPrefix(a, "--consumer=")
		case a == "--rail":
			i++
			if i >= len(args) {
				return fmt.Errorf("--rail requires id")
			}
			rail = args[i]
		case strings.HasPrefix(a, "--rail="):
			rail = strings.TrimPrefix(a, "--rail=")
		case a == "--watts":
			i++
			if i >= len(args) {
				return fmt.Errorf("--watts requires a number")
			}
			if _, err := fmt.Sscanf(args[i], "%f", &watts); err != nil {
				return fmt.Errorf("invalid --watts")
			}
		case strings.HasPrefix(a, "--watts="):
			if _, err := fmt.Sscanf(strings.TrimPrefix(a, "--watts="), "%f", &watts); err != nil {
				return fmt.Errorf("invalid --watts")
			}
		case a == "--envelope":
			i++
			if i >= len(args) {
				return fmt.Errorf("--envelope requires a path")
			}
			envelopeFile = args[i]
		case strings.HasPrefix(a, "--envelope="):
			envelopeFile = strings.TrimPrefix(a, "--envelope=")
		case a == "--preset":
			i++
			if i >= len(args) {
				return fmt.Errorf("--preset requires a name")
			}
			envelopePreset = args[i]
		case strings.HasPrefix(a, "--preset="):
			envelopePreset = strings.TrimPrefix(a, "--preset=")
		case a == "-h" || a == "--help":
			fmt.Print(powerUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	sim := power.NewSimulator()
	var b power.Budget
	switch {
	case envelopeFile != "" || envelopePreset != "":
		var envYAML []byte
		var err error
		if envelopeFile != "" {
			env, err2 := envelope.LoadFile(envelopeFile)
			if err2 != nil {
				return err2
			}
			envYAML, err = env.MarshalYAML()
		} else {
			env, err2 := envelope.Preset(envelopePreset)
			if err2 != nil {
				return err2
			}
			envYAML, err = env.MarshalYAML()
		}
		if err != nil {
			return err
		}
		intensity := power.IntensityFromEnvelopeYAML(envYAML)
		b = power.EnvelopeBudgetHint(consumer, intensity, sim.State().TotalBudgetW, 0)
		b.RailID = rail
	default:
		b = power.Budget{ConsumerID: consumer, RailID: rail, Watts: watts, PeakWatts: watts * 1.4, Source: "manual"}
	}
	resp := sim.Handle(power.Message{Type: power.MsgSetBudget, SchemaVersion: power.SchemaVersion, Budget: &b})
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	for _, w := range resp.State.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	fmt.Printf("ok budget consumer=%s rail=%s watts=%.1f peak=%.1f source=%s\n",
		b.ConsumerID, b.RailID, b.Watts, b.PeakWatts, b.Source)
	raw, err := power.MarshalStateYAML(*resp.State)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(raw)
	return err
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
  omls daemon [--data-dir PATH] [--listen :7443]
  omls status
  omls nodes
  omls cluster
  omls graph [--out graph.yaml]
  omls run-demo [--workers 8] [--policy adaptive]
  omls agent discover [--out machine-profile.yaml] [--format yaml|json] [--sandbox]
  omls agent run --master HOST:PORT [--ca --cert --key | --insecure]
  omls agent register --master HOST:PORT [...]
  omls agent envelope validate|show|apply [--preset NAME | --file PATH]
  omls agent sandbox probe|list|claim
  omls master serve [--listen :7443] [--data-dir omls-data] [--community-dir DIR] [--ca --cert --key | --insecure]
  omls master graph --master HOST:PORT --out graph.yaml
  omls master health --master HOST:PORT
  omls master profiles list|show --master HOST:PORT [...]
  omls master run-demo --master HOST:PORT [--workers 8] [--preset eco] [--policy ewma|adaptive]
  omls community list|show|import [--dir DIR]
  omls power show|hello|budget
  omls update [--check] [--force]
  omls version

OMLS 1.1: nodes discover each other and form a cluster. master/agent remain for debug.
`)
}

var errDaemonStopped = errors.New("daemon stopped")

func agentUsage() string {
	return `omls agent — local node agent

Usage:
  omls agent discover [flags]
  omls agent run [flags]
  omls agent register [flags]
  omls agent envelope [validate|show|apply]
  omls agent sandbox [probe|list|claim]

Commands:
  discover   Probe sysfs/proc and write an RDL Machine Profile
  run        Discover, register, advertise, heartbeat, and execute work
  register   One-shot register + advertise + heartbeat
  envelope   Validate/show/apply a Resource Envelope (0.4)
  sandbox    VFIO/UIO sandbox probe and dry-run claim (0.8)
`
}

func sandboxUsage() string {
	return `omls agent sandbox — VFIO/UIO sandbox stub (0.8)

Usage:
  omls agent sandbox probe [--sys-root DIR]
  omls agent sandbox list [--sys-root DIR]
  omls agent sandbox claim ID|--id ID [--sys-root DIR]

Probe is best-effort against /sys and /dev. Typical cloud/lab VMs have no
VFIO/UIO — OMLS warns and never crashes. claim is always simulated (no real
driver bind/unbind).
`
}

func envelopeUsage() string {
	return `omls agent envelope — Resource Envelope tools (0.4)

Usage:
  omls agent envelope validate [--preset balanced|high|eco | --file PATH]
  omls agent envelope show [--preset NAME | --file PATH]
  omls agent envelope apply [--preset NAME | --file PATH] [--duration 2s]

Apply uses best-effort backends: userspace duty-cycle (always), cgroup v2
cpu.max and CPUFreq scaling_max_freq when writable.
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
  omls agent discover [--out PATH] [--format yaml|json] [--quiet] [--sandbox]

Flags:
  --out, -o PATH     Output path (default: machine-profile.yaml)
  --format FORMAT    yaml or json (default: from --out suffix)
  --quiet, -q        Suppress warnings on stderr
  --sandbox          Merge VFIO/UIO sandbox resources into the profile (0.8)
`
}

func masterUsage() string {
	return `omls master — Resource Graph coordinator

Usage:
  omls master serve [flags]
  omls master graph [flags]
  omls master health [flags]
  omls master profiles list|show [flags]
  omls master run-demo [flags]
`
}

func masterProfilesUsage() string {
	return `omls master profiles — Behavior Profiles (0.5)

Usage:
  omls master profiles list --master HOST:PORT (--ca --cert --key | --insecure)
  omls master profiles show --master HOST:PORT --node NODE_ID [--out profile.yaml]
       [--telemetry-tail 20] [--telemetry-out samples.jsonl] (--ca --cert --key | --insecure)

Profiles are written under the master's --data-dir (default omls-data) from
heartbeats/telemetry and run-demo ObserveWork updates.
`
}

func masterDemoUsage() string {
	return `omls master run-demo — schedule parallel_workers and learn

Usage:
  omls master run-demo --master HOST:PORT [--workers 8] [--iterations 3] [--work-iterations N]
       [--device cpu|gpu] [--preset eco|--envelope FILE] [--policy ewma|adaptive] (--ca --cert --key | --insecure)

Agents must be running (` + "`omls agent run`" + `) so they can claim and execute work units.
Optional --preset/--envelope attaches a Resource Envelope to each work unit (0.4).
--policy ewma (default): 0.3 thermal + duration EWMA rules.
--policy adaptive (0.6): multi-signal blend + hysteresis/cooldown; explainable score notes.
--device gpu schedules one work unit per OpenCL GPU and executes them serially per agent.
GPU mode requires a working OpenCL GPU runtime on the agent; envelopes are not supported yet.
Increase --work-iterations to increase compute work per GPU.
With one node the demo still runs; rebalancing needs ≥2 available nodes.
`
}

func communityUsage() string {
	return `omls community — shared hardware/behavior profile snippets (0.7)

Usage:
  omls community list [--dir DIR]
  omls community show ID [--dir DIR]
  omls community import PATH [--dir DIR]

Default --dir resolves to the first existing of: community/, profiles/, examples/community/.
Import validates YAML and copies into --dir. Master --community-dir applies matching
priors on AdvertiseProfile when the node has no local Behavior observations yet.
`
}

func powerUsage() string {
	return `omls power — Physical Power Fabric / MCU simulator (0.9)

Usage:
  omls power show
  omls power hello
  omls power budget [--consumer ID] [--rail rail-12v] [--watts N]
  omls power budget --preset eco|--envelope FILE [--consumer ID]

No real MCU hardware. show/hello talk to an in-process simulator.
budget can derive soft watt hints from Resource Envelopes (0.4).
Master run-demo attaches envelope budgets to its in-process fabric when --preset/--envelope is set.
`
}

func masterServeUsage() string {
	return `omls master serve — run the fabric control plane

Usage:
  omls master serve [--listen :7443] [--data-dir omls-data] [--community-dir DIR] [--heartbeat-timeout 15s] (--ca CA --cert CERT --key KEY | --insecure)

Flags:
  --listen ADDR              Bind address (default :7443)
  --data-dir PATH            Behavior Profiles + telemetry store (default omls-data)
  --community-dir PATH       Community profile catalog (default: community/ or examples/community/)
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
