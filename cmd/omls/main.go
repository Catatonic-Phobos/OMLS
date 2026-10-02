// Command omls is the Operational Machine Learning System CLI.
// OMLS 0.1 ships only `omls agent discover`.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/discover"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "agent":
		if err := runAgent(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "omls agent: %v\n", err)
			os.Exit(1)
		}
	case "version", "--version", "-V":
		fmt.Printf("omls %s\n", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func runAgent(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected subcommand (discover)")
	}
	switch args[0] {
	case "discover":
		return runDiscover(args[1:])
	case "help", "-h", "--help":
		fmt.Print(agentUsage())
		return nil
	default:
		return fmt.Errorf("unknown agent subcommand %q (try: discover)", args[0])
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

func usage() {
	fmt.Print(`omls — Operational Machine Learning System

Usage:
  omls agent discover [--out machine-profile.yaml] [--format yaml|json]
  omls version
  omls help

OMLS 0.1: local hardware discovery → RDL Machine Profile.
Master / fabric / scheduler arrive in later releases.
`)
}

func agentUsage() string {
	return `omls agent — local node agent

Usage:
  omls agent discover [flags]

Commands:
  discover   Probe sysfs/proc and write an RDL Machine Profile
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
