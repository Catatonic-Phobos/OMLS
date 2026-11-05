package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/borrow"
	"github.com/Catatonic-Phobos/OMLS/internal/cache"
)

func runBorrow(args []string) error {
	planOnly := false
	for _, a := range args {
		switch a {
		case "--plan":
			planOnly = true
		case "-h", "--help":
			fmt.Print(borrowUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	self := borrow.FromHeadroom(cache.SampleHeadroom(), "")
	if self.Name == "" {
		self.Name, _ = os.Hostname()
	}
	peers, err := borrowPeers(self.Name)
	if err != nil {
		return err
	}
	decision := borrow.Decide(self, peers)
	fmt.Print(borrow.Format(decision))
	if planOnly {
		return nil
	}
	var state borrow.State
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, line := range state.Sync(ctx, self, peers, borrow.CacheClient{}) {
		fmt.Println(line)
	}
	return nil
}

func borrowPeers(self string) ([]borrow.Offer, error) {
	listed, err := peerList()
	if err != nil {
		return nil, err
	}
	var out []borrow.Offer
	for _, p := range listed {
		if strings.EqualFold(p.name, self) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
		h, err := cache.FetchHeadroom(ctx, p.cache)
		cancel()
		if err != nil {
			continue
		}
		o := borrow.FromHeadroom(h, p.cache)
		if o.Name == "" {
			o.Name = p.name
		}
		out = append(out, o)
	}
	return out, nil
}

func borrowUsage() string {
	return `omls borrow — place texture or shader RAM on a peer (1.4)

Usage:
  omls borrow [--plan]

When this machine's CPU or RAM reaches 80%, texture bytes are reserved on the
peer with the most free RAM. When GPU memory reaches 80%, shader bytes are
reserved on the peer with the most free VRAM. The daemon does this on its own.
--plan prints the assignment without placing it.

The draw stays on the GPU that owns the frame. The peer holds the copy.
`
}
