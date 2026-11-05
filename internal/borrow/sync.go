package borrow

import (
	"context"
	"fmt"

	"github.com/Catatonic-Phobos/OMLS/internal/cache"
)

// Hold is a slice this machine has already placed.
type Hold struct {
	Peer string
	Addr string
	Name string
}

// State remembers placements so a function stays put until this machine
// drops under the release line, or the chosen peer changes.
type State struct {
	Holds map[string]Hold
}

// Client places and releases named slices.
type Client interface {
	Place(ctx context.Context, addr, name, kind string) error
	Release(ctx context.Context, addr, name string) error
}

// CacheClient dials a peer's cache directly.
type CacheClient struct{}

func (CacheClient) Place(ctx context.Context, addr, name, kind string) error {
	n := FillBytes
	if n > ReserveBytes {
		n = ReserveBytes
	}
	payload := make([]byte, n)
	copy(payload, []byte("omls-borrow:"+kind))
	_, err := cache.PlaceNamed(ctx, addr, name, ReserveBytes, cache.Slice{
		Kind:   kind,
		Source: "borrow:" + kind,
		Data:   payload,
	})
	return err
}

func (CacheClient) Release(ctx context.Context, addr, name string) error {
	_, err := cache.ReleaseNamed(ctx, addr, name)
	return err
}

// Sync applies Decide. It returns one log line per place, release, or error.
func (s *State) Sync(ctx context.Context, self Offer, peers []Offer, c Client) []string {
	if s.Holds == nil {
		s.Holds = map[string]Hold{}
	}
	dec := Decide(self, peers)
	want := map[string]Placement{}
	for _, p := range dec.Placements {
		want[p.Function] = p
	}
	var lines []string
	for _, fn := range []string{FunctionTexture, FunctionShader} {
		next, ok := want[fn]
		have := s.Holds[fn]
		switch {
		case ok && have.Addr != next.Addr:
			name := slotName(self.Name, fn)
			if err := c.Place(ctx, next.Addr, name, next.Kind); err != nil {
				lines = append(lines, fmt.Sprintf("borrow %s → %s: %v", fn, next.Peer, err))
				continue
			}
			if have.Addr != "" && have.Addr != next.Addr {
				_ = c.Release(ctx, have.Addr, have.Name)
			}
			s.Holds[fn] = Hold{Peer: next.Peer, Addr: next.Addr, Name: name}
			lines = append(lines, fmt.Sprintf("borrow %s → %s", fn, next.Peer))
		case !ok && have.Addr != "" && !StillHot(self, fn):
			if err := c.Release(ctx, have.Addr, have.Name); err != nil {
				lines = append(lines, fmt.Sprintf("borrow release %s: %v", fn, err))
				continue
			}
			delete(s.Holds, fn)
			lines = append(lines, fmt.Sprintf("borrow release %s", fn))
		}
	}
	return lines
}

func slotName(host, function string) string {
	if host == "" {
		host = "node"
	}
	return host + "/" + function
}
