package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/storage"
)

func runStorage(args []string) error {
	if len(args) == 0 {
		fmt.Print(storageHelp)
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("storage "+args[0], flag.ContinueOnError)
	config := fs.String("env", filepath.Join(home, ".config/omls/connections.env"), "private connection configuration")
	node := fs.String("node", "STUDIO", "connection profile")
	remote := fs.String("path", "", "relative path inside the remote cache root")
	source := fs.String("source", "", "local file to upload")
	output := fs.String("out", "", "local destination for download")
	cache := fs.String("cache-dir", filepath.Join(home, ".cache/omls/assets"), "local content cache")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if args[0] == "init" {
		return storage.Init(*config, home)
	}
	p, err := storage.Load(*config, *node)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	switch args[0] {
	case "check":
		return p.Check(ctx)
	case "login":
		return p.Login(ctx)
	case "put":
		return p.Put(ctx, *source, *remote)
	case "get":
		r, err := p.Get(ctx, *remote, *output, *cache)
		if err != nil {
			return err
		}
		fmt.Printf("node=%s protocol=%s bytes=%d sha256=%s local_cache_hit=%t payload_downloaded=%d duration=%s\n", *node, p.Mode, r.Bytes, r.Hash, r.Hit, r.Downloaded, r.Duration.Round(time.Millisecond))
		return nil
	default:
		return fmt.Errorf("unknown storage command %q", args[0])
	}
}

const storageHelp = `omls storage — persistent LAN connections and real file cache

  omls storage init
  omls storage login --node STUDIO
  omls storage check --node STUDIO
  omls storage put --node STUDIO --source FILE --path assets/FILE
  omls storage get --node STUDIO --path assets/FILE --out FILE

Default private profile: ~/.config/omls/connections.env (0600).
SSH uses a persistent key; login runs ssh-copy-id interactively once.
SMB uses credentials from the private profile, never command-line passwords.
SSH cache hits validate the remote SHA-256 before reusing local bytes.
SMB downloads before deduplication; it does not claim network cache savings.
These commands share files; they do not redirect an already running game.
`
