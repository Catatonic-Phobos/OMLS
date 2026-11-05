package cluster

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/borrow"
	"github.com/Catatonic-Phobos/OMLS/internal/cache"
)

func (d *Daemon) runBorrow(ctx context.Context) {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	var state borrow.State
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			d.borrowPass(ctx, &state)
			timer.Reset(5 * time.Second)
		}
	}
}

func (d *Daemon) borrowPass(ctx context.Context, state *borrow.State) {
	self := borrow.FromHeadroom(cache.SampleHeadroom(), "")
	if self.Name == "" {
		self.Name = d.self.Hostname
	}
	var peers []borrow.Offer
	for _, m := range d.members.Snapshot() {
		if m.NodeID == d.self.NodeID || m.Addr == "" || m.Status != StatusReady {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
		h, err := cache.FetchHeadroom(cctx, cache.CachePort(m.Addr))
		cancel()
		if err != nil {
			continue
		}
		o := borrow.FromHeadroom(h, cache.CachePort(m.Addr))
		if o.Name == "" {
			o.Name = m.Hostname
		}
		peers = append(peers, o)
	}
	pctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for _, line := range state.Sync(pctx, self, peers, borrow.CacheClient{}) {
		fmt.Fprintf(os.Stderr, "omlsd: %s\n", line)
	}
}
