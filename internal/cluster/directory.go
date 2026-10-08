package cluster

import (
	"context"
	"sync"
)

const (
	// EventJoin is a newly observed peer.
	EventJoin = "join"
	// EventUpdate refreshes a peer that is still present.
	EventUpdate = "update"
	// EventLeave means the peer withdrew or disappeared.
	EventLeave = "leave"
)

// Peer is the advertisement every OMLS node publishes on the LAN.
type Peer struct {
	NodeID          string
	Hostname        string
	OS              string
	Virt            string
	Host            string
	Port            int
	ProtocolVersion string
}

// Event is one membership change from a Directory.
type Event struct {
	Kind string
	Peer Peer
}

// Directory advertises this node and reports other OMLS nodes.
type Directory interface {
	Announce(ctx context.Context, self Peer) error
	Watch(ctx context.Context) (<-chan Event, error)
}

// MemoryDirectory is an in-process LAN substitute for tests.
type MemoryDirectory struct {
	mu    sync.Mutex
	peers map[string]Peer
	subs  map[int]chan Event
	next  int
}

// NewMemoryDirectory returns an empty shared discovery bus.
func NewMemoryDirectory() *MemoryDirectory {
	return &MemoryDirectory{
		peers: map[string]Peer{},
		subs:  map[int]chan Event{},
	}
}

// Announce publishes self until ctx is cancelled, then withdraws.
func (d *MemoryDirectory) Announce(ctx context.Context, self Peer) error {
	d.mu.Lock()
	d.peers[self.NodeID] = self
	subs := d.subscriberChans()
	d.mu.Unlock()
	ev := Event{Kind: EventJoin, Peer: self}
	for _, ch := range subs {
		offer(ch, ev)
	}
	go func() {
		<-ctx.Done()
		d.withdraw(self.NodeID)
	}()
	return nil
}

// Watch reports peers already present and later joins and leaves.
func (d *MemoryDirectory) Watch(ctx context.Context) (<-chan Event, error) {
	ch := make(chan Event, 32)
	d.mu.Lock()
	id := d.next
	d.next++
	d.subs[id] = ch
	existing := make([]Peer, 0, len(d.peers))
	for _, p := range d.peers {
		existing = append(existing, p)
	}
	d.mu.Unlock()
	for _, p := range existing {
		offer(ch, Event{Kind: EventJoin, Peer: p})
	}
	go func() {
		<-ctx.Done()
		d.mu.Lock()
		delete(d.subs, id)
		d.mu.Unlock()
	}()
	return ch, nil
}

func (d *MemoryDirectory) withdraw(id string) {
	d.mu.Lock()
	p, ok := d.peers[id]
	delete(d.peers, id)
	subs := d.subscriberChans()
	d.mu.Unlock()
	if !ok {
		return
	}
	ev := Event{Kind: EventLeave, Peer: p}
	for _, ch := range subs {
		offer(ch, ev)
	}
}

func (d *MemoryDirectory) subscriberChans() []chan Event {
	out := make([]chan Event, 0, len(d.subs))
	for _, ch := range d.subs {
		out = append(out, ch)
	}
	return out
}

func offer(ch chan Event, ev Event) {
	select {
	case ch <- ev:
	default:
	}
}
