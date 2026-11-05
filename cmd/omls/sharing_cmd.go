package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"syscall"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/sharing"
	"github.com/Catatonic-Phobos/OMLS/internal/storage"
)

func runSharing(args []string) error {
	if len(args) == 0 {
		fmt.Print(sharingHelp)
		return nil
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	fs := flag.NewFlagSet("sharing "+args[0], flag.ContinueOnError)
	root := fs.String("root", filepath.Join(home, "OMLS/sharing"), "per-machine sharing root")
	specFile := fs.String("spec", "", "task specification JSON")
	source := fs.String("source", "", "completed result to publish")
	out := fs.String("out", "", "consumer output path")
	key := fs.String("key", "", "task key to retrieve")
	budget := fs.Int64("max-bytes", 4<<30, "persistent cache byte budget")
	minFree := fs.Uint64("min-free-bytes", 1<<30, "minimum filesystem free space")
	ttl := fs.Duration("compute-ttl", 24*time.Hour, "compute result TTL from publication; graphics has no TTL")
	node := fs.String("node", "STUDIO", "remote execution profile")
	connections := fs.String("env", filepath.Join(home, ".config/omls/connections.env"), "private connection configuration")
	remoteBinary := fs.String("remote-binary", "", "installed remote OMLS executable")
	ownerPID := fs.Int("owner-pid", 0, "cancel this task when its owning local application exits")
	leaseStdin := fs.Bool("lease-stdin", false, "internal SSH task heartbeat transport")
	if e := fs.Parse(args[1:]); e != nil {
		return e
	}
	if fs.NArg() != 0 && args[0] != "run" && args[0] != "dispatch" {
		return fmt.Errorf("unexpected positional arguments")
	}
	var spec sharing.Spec
	var leaseReader *bufio.Reader
	if *specFile != "" {
		var f *os.File
		if *specFile == "-" {
			f = os.Stdin
		} else {
			f, e = os.Open(*specFile)
		}
		if e != nil {
			return e
		}
		if *leaseStdin {
			if *specFile != "-" {
				return fmt.Errorf("heartbeat transport requires --spec -")
			}
			leaseReader = bufio.NewReaderSize(f, 1<<20)
			var line []byte
			line, e = leaseReader.ReadSlice('\n')
			if e == nil {
				spec, e = sharing.DecodeSpec(bytes.NewReader(line))
			}
		} else {
			spec, e = sharing.DecodeSpec(f)
		}
		if f != os.Stdin {
			f.Close()
		}
		if e != nil {
			return e
		}
		derived, e := spec.Key()
		if e != nil {
			return e
		}
		if *key != "" && *key != derived {
			return fmt.Errorf("key does not match spec")
		}
		*key = derived
	}
	if args[0] == "key" {
		if *key == "" {
			return fmt.Errorf("--spec required")
		}
		fmt.Println(*key)
		return nil
	}
	switch args[0] {
	case "init", "put", "get", "gc", "status", "run", "dispatch", "maintain":
	default:
		return fmt.Errorf("unknown sharing command %q", args[0])
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, ownerStop, e := watchSharingOwner(ctx, *ownerPID)
	if e != nil {
		return e
	}
	defer ownerStop()
	if leaseReader != nil {
		var cancel context.CancelFunc
		ctx, cancel = watchSharingLease(ctx, leaseReader, 25*time.Second)
		defer cancel()
		defer os.Stdin.Close()
	}
	policy := sharing.Policy{MaxBytes: *budget, MinFreeBytes: *minFree, ComputeTTL: *ttl}
	policyFile := filepath.Join(*root, "sharing-policy.json")
	if b, e := os.ReadFile(policyFile); e == nil {
		if e = json.Unmarshal(b, &policy); e != nil {
			return fmt.Errorf("invalid stored sharing policy: %w", e)
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "max-bytes":
			policy.MaxBytes = *budget
		case "min-free-bytes":
			policy.MinFreeBytes = *minFree
		case "compute-ttl":
			policy.ComputeTTL = *ttl
		}
	})
	store, e := sharing.Open(ctx, *root, policy)
	if e != nil {
		return e
	}
	if args[0] == "init" {
		if e := store.SavePolicy(ctx); e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"root": store.Root, "policy": policy})
	}
	var result any
	switch args[0] {
	case "maintain":
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			if b, err := os.ReadFile(policyFile); err == nil {
				var fresh sharing.Policy
				if err = json.Unmarshal(b, &fresh); err != nil {
					return err
				}
				if fresh.MaxBytes <= 0 || fresh.ComputeTTL < 0 {
					return fmt.Errorf("invalid sharing policy")
				}
				store.Policy = fresh
			} else if !os.IsNotExist(err) {
				return err
			}
			report, err := store.GC(ctx)
			if err != nil && ctx.Err() == nil {
				fmt.Fprintln(os.Stderr, "sharing maintenance:", err)
			} else if report.RemovedTemps+report.RemovedObjects+report.RemovedManifests > 0 {
				_ = json.NewEncoder(os.Stdout).Encode(report)
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	case "run", "dispatch":
		if *specFile == "" || *out == "" || fs.NArg() == 0 {
			return fmt.Errorf("--spec, --out and command arguments after -- required")
		}
		var params map[string]json.RawMessage
		_ = json.Unmarshal(spec.Parameters, &params)
		var argv []string
		if json.Unmarshal(params["argv"], &argv) != nil || !reflect.DeepEqual(argv, fs.Args()) {
			return fmt.Errorf("spec parameters.argv must match the exact producer command")
		}
		var writer io.Writer = os.Stdout
		var output *os.File
		if *out != "-" {
			if e = os.MkdirAll(filepath.Dir(*out), 0700); e != nil {
				return e
			}
			output, e = os.CreateTemp(filepath.Dir(*out), ".sharing-result-*")
			if e != nil {
				return e
			}
			defer output.Close()
			defer os.Remove(output.Name())
			writer = output
		}
		producer := func(ctx context.Context, path string) error {
			c := exec.CommandContext(ctx, argv[0], argv[1:]...)
			c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
			c.Cancel = func() error {
				err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
				if err == syscall.ESRCH {
					return os.ErrProcessDone
				}
				return err
			}
			c.WaitDelay = time.Second
			c.Env = append(os.Environ(), "OMLS_RESULT_PATH="+path, "OMLS_SHARING_ROOT="+store.Root, "OMLS_TASK_MANIFEST="+filepath.Join(filepath.Dir(path), "task.json"), "OMLS_TASK_KEY="+*key)
			c.Stdout = os.Stderr
			c.Stderr = os.Stderr
			return c.Run()
		}
		if args[0] == "dispatch" {
			p, err := storage.Load(*connections, *node)
			if err != nil {
				return err
			}
			binary := *remoteBinary
			if binary == "" {
				binary = filepath.Join(p.Root, "runtime/omls")
			}
			producer = func(ctx context.Context, path string) error {
				command := []string{binary, "sharing", "run", "--root", p.Root, "--spec", "-", "--out", "-", "--lease-stdin", "--"}
				command = append(command, argv...)
				c, err := p.Exec(ctx, command...)
				if err != nil {
					return err
				}
				document, err := spec.Canonical()
				if err != nil {
					return err
				}
				f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err != nil {
					return err
				}
				c.Stderr = os.Stderr
				err = sharingRemoteResult(ctx, c, document, f)
				ce := f.Close()
				if err != nil {
					return err
				}
				return ce
			}
		}
		m, hit, err := store.Resolve(ctx, spec, writer, producer)
		if err != nil {
			return err
		}
		if output != nil {
			if err = output.Sync(); err != nil {
				return err
			}
			if err = output.Close(); err != nil {
				return err
			}
			if err = os.Rename(output.Name(), *out); err != nil {
				return err
			}
		}
		result = map[string]any{"cache_hit": hit, "result": m}
		if *out == "-" {
			return json.NewEncoder(os.Stderr).Encode(result)
		}
	case "put":
		if *specFile == "" || *source == "" {
			return fmt.Errorf("--spec and --source required")
		}
		f, e := os.Open(*source)
		if e != nil {
			return e
		}
		defer f.Close()
		m, e := store.Put(ctx, spec, f)
		if e != nil {
			return e
		}
		result = m
	case "get":
		if *key == "" || *out == "" {
			return fmt.Errorf("--spec or --key, and --out required")
		}
		if e = os.MkdirAll(filepath.Dir(*out), 0700); e != nil {
			return e
		}
		f, e := os.CreateTemp(filepath.Dir(*out), ".sharing-get-*")
		if e != nil {
			return e
		}
		defer os.Remove(f.Name())
		m, e := store.Get(ctx, *key, f)
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		if e = os.Rename(f.Name(), *out); e != nil {
			return e
		}
		result = map[string]any{"cache_hit": true, "result": m}
	case "gc":
		result, e = store.GC(ctx)
	case "status":
		result, e = store.Status(ctx)
	}
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

const sharingHelp = `omls sharing — task result cache with its own lifecycle

  omls sharing init [--root ~/OMLS/sharing] [--max-bytes N]
  omls sharing key --spec task.json
  omls sharing put --spec task.json --source completed-result
  omls sharing get --spec task.json --out consumer-file
  omls sharing status
  omls sharing gc
  omls sharing maintain
  omls sharing run --spec task.json --out result -- COMMAND [ARG...]
  omls sharing dispatch --node STUDIO --spec task.json --out result -- COMMAND [ARG...]

tmp/ holds leased task output; cache/graphics and cache/compute hold content
objects; manifests/ maps task specifications to complete results. Compute TTL
defaults to 24h; graphics has no TTL. Both obey the byte budget and LRU eviction.
init persists policy; explicit flags override it for an invocation.
Closing an application does not delete valid results. Startup recovers abandoned
task directories. This cache does not yet intercept or render Sims graphics.
`
