package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Catatonic-Phobos/OMLS/internal/update"
)

func runUpdate(args []string) error {
	opts := update.Options{
		Repo:           update.DefaultRepo,
		CurrentVersion: version,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--check":
			opts.CheckOnly = true
		case a == "--force":
			opts.Force = true
		case a == "--repo":
			i++
			if i >= len(args) {
				return fmt.Errorf("--repo requires owner/name")
			}
			opts.Repo = args[i]
		case strings.HasPrefix(a, "--repo="):
			opts.Repo = strings.TrimPrefix(a, "--repo=")
		case a == "-h" || a == "--help":
			fmt.Print(updateUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	if opts.Repo == "" || !strings.Contains(opts.Repo, "/") {
		return fmt.Errorf("invalid --repo %q (want owner/name)", opts.Repo)
	}
	_, err := update.Run(opts)
	return err
}

func updateUsage() string {
	return `omls update — download the latest GitHub release pack and install it

Usage:
  omls update [--check] [--force] [--repo owner/name]

Fetches the latest release from https://github.com/Catatonic-Phobos/OMLS,
downloads omls-linux-<arch>.tar.gz, and runs the pack installer (binary +
omls.service). Node identity under /var/lib/omls is preserved.

Flags:
  --check          Report whether an update is available; do not install
  --force          Reinstall even when versions match
  --repo owner/name  Override GitHub repository (default Catatonic-Phobos/OMLS)

The SanDisk / field image needs one binary that already includes this command.
After that, omls update keeps the machine current. Releases must attach the
pack asset (see scripts/pack-release.sh and docs/installation.md).
`
}
