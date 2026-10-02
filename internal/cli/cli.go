// Copyright 2026 Admar Sabaz. SPDX-License-Identifier: MIT

// Package cli implements the omls command line.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Catatonic-Phobos/OMLS/internal/discover"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
	"github.com/Catatonic-Phobos/OMLS/internal/version"
)

const usage = `OMLS inventories a Linux node and writes a Resource Description (RDL 0.1).

Usage:
  omls version
  omls agent discover [flags]

Run 'omls agent discover --help' for discover flags.
`

const discoverUsage = `Usage:
  omls agent discover [flags]

Read the local Linux node from /proc, /sys, and /etc and write an RDL 0.1
document. Version 0.1 uses stock userspace only.

Flags:
  --root PATH     Read proc, sys, and etc under PATH instead of /
  --output PATH   Write the document to PATH instead of stdout
  --compact       Print one line of JSON
`

// Run executes one omls invocation. args is os.Args without the program name.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintf(stdout, "omls %s\n", version.Version)
		return 0
	case "agent":
		return runAgent(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "omls: unknown command %q\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}
}

func runAgent(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, "omls: missing agent command\n")
		fmt.Fprint(stderr, discoverUsage)
		return 2
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, discoverUsage)
		return 0
	case "discover":
		return runDiscover(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "omls: unknown agent command %q\n", args[0])
		fmt.Fprint(stderr, discoverUsage)
		return 2
	}
}

func runDiscover(args []string, stdout, stderr io.Writer) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Fprint(stdout, discoverUsage)
			return 0
		}
	}
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, discoverUsage)
	}
	root := fs.String("root", "/", "filesystem root for proc, sys, and etc")
	output := fs.String("output", "", "write the document to this path")
	compact := fs.Bool("compact", false, "print one line of JSON")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "omls: unexpected argument %q\n", fs.Arg(0))
		fmt.Fprint(stderr, discoverUsage)
		return 2
	}

	doc, err := discover.Discover(discover.Options{Root: *root})
	if err != nil {
		fmt.Fprintf(stderr, "omls: %v\n", err)
		return 1
	}
	payload, err := rdl.Marshal(doc, *compact)
	if err != nil {
		fmt.Fprintf(stderr, "omls: %v\n", err)
		return 1
	}
	if *output == "" || *output == "-" {
		if _, err := stdout.Write(payload); err != nil {
			fmt.Fprintf(stderr, "omls: %v\n", err)
			return 1
		}
		return 0
	}
	if err := writeAtomic(*output, payload); err != nil {
		fmt.Fprintf(stderr, "omls: %v\n", err)
		return 1
	}
	return 0
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".omls-discover-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
