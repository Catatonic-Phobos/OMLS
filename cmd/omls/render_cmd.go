package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/renderstream"
	"github.com/Catatonic-Phobos/OMLS/internal/storage"
)

type renderProbe struct {
	Backend           string `json:"backend"`
	Initialized       bool   `json:"backend_initialized"`
	Device            string `json:"device"`
	Driver            string `json:"driver"`
	API               uint32 `json:"api_version"`
	DriverVersion     uint32 `json:"driver_version"`
	Vendor            uint32 `json:"vendor_id"`
	DeviceID          uint32 `json:"device_id"`
	Memory            uint64 `json:"device_local_heap_bytes"`
	ICDReady          bool   `json:"application_icd_ready"`
	PresentationReady bool   `json:"presentation_ready"`
	Multiplexed       bool   `json:"multiplexed_session_ready"`
	ProtocolVersion   int    `json:"protocol_version"`
	ExecutorPID       int    `json:"executor_pid"`
}

func validateRenderProbe(p renderProbe, expected string) error {
	if p.Backend != "gfxstream" || !p.Initialized || p.Device == "" || p.API == 0 {
		return fmt.Errorf("remote Vulkan backend did not initialize")
	}
	if expected != "" && !strings.Contains(p.Device, expected) {
		return fmt.Errorf("remote Vulkan device does not match requested executor")
	}
	return nil
}
func runRender(args []string) error {
	if len(args) == 0 {
		fmt.Print(renderHelp)
		return nil
	}
	if args[0] != "probe" && args[0] != "channel" && args[0] != "session" {
		return fmt.Errorf("expected render probe, channel or session; windowed application presentation is not available yet")
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	fs := flag.NewFlagSet("render "+args[0], flag.ContinueOnError)
	config := fs.String("env", filepath.Join(home, ".config/omls/connections.env"), "private connection profiles")
	node := fs.String("node", "STUDIO", "GPU executor profile")
	expected := fs.String("expect", "", "required substring in hardware device name")
	icd := fs.String("icd", "/usr/share/vulkan/icd.d/nvidia_icd.json", "remote hardware ICD")
	backend := fs.String("backend", "", "remote native adapter path")
	socket := fs.String("socket", "", "local private Unix socket for a multiplexed session")
	root := fs.String("root", filepath.Join(home, "OMLS/sharing"), "local sharing root")
	owner := fs.Int("owner-pid", 0, "cancel session when this local owning application exits")
	trace := fs.Bool("trace", false, "log decoded native Vulkan calls on the executor")
	if e = fs.Parse(args[1:]); e != nil {
		return e
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	p, e := storage.Load(*config, *node)
	if e != nil {
		return e
	}
	binary := *backend
	if binary == "" {
		binary = filepath.Join(p.Root, "runtime/omls-gfxstream-host")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	if args[0] == "session" {
		ctx, done, err := watchSharingOwner(ctx, *owner)
		if err != nil {
			return err
		}
		defer done()
		return runRenderSession(ctx, p, binary, *icd, *expected, *root, *socket, *trace)
	}
	mode := "--stdio"
	if args[0] == "probe" {
		mode = "--probe"
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Minute)
		defer cancel()
	}
	command := []string{binary, mode, "--icd", *icd}
	if *trace {
		command = append(command, "--trace")
	}
	if *expected != "" {
		command = append(command, "--expect", *expected)
	}
	c, e := p.Exec(ctx, command...)
	if e != nil {
		return e
	}
	c.Stderr = os.Stderr
	if args[0] == "channel" {
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		return c.Run()
	}
	output, e := c.Output()
	if e != nil {
		return fmt.Errorf("remote render probe failed: %w", e)
	}
	var report renderProbe
	if e = json.Unmarshal(output, &report); e != nil {
		return fmt.Errorf("invalid render backend response: %w", e)
	}
	if e = validateRenderProbe(report, *expected); e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}

const renderHelp = `omls render — native Gfxstream host over authenticated SSH

  omls render probe --node STUDIO --expect "GTX 1050"
  omls render channel --node STUDIO
  omls render session --node STUDIO [--socket PATH] [--owner-pid PID]

probe initializes the native decoder and queries the hardware actually selected.
channel carries a single native Gfxstream wire channel on stdin/stdout.
session relays multiple local Unix streams through one shared remote device.
The application Vulkan ICD, memory adaptation and frame presentation are still
experimental/incomplete. This is not a game launch command.
`

// Assert protocol agreement before publishing a local socket as ready.
func validateRenderSession(p renderProbe, expected string) error {
	if e := validateRenderProbe(p, expected); e != nil {
		return e
	}
	if !p.Multiplexed || p.ProtocolVersion != renderstream.Version {
		return fmt.Errorf("incompatible remote graphics session")
	}
	return nil
}
