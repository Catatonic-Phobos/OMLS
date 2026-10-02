// Package graph is the in-memory OMLS Resource Graph (0.2).
package graph

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
	"gopkg.in/yaml.v3"
)

// Status is node availability in the graph.
type Status string

const (
	StatusAvailable   Status = "available"
	StatusUnavailable Status = "unavailable"
)

// Telemetry is the last light sample from a node.
type Telemetry struct {
	CPULoad           float64   `json:"cpu_load" yaml:"cpu_load"`
	MemAvailableBytes int64     `json:"mem_available_bytes" yaml:"mem_available_bytes"`
	MemTotalBytes     int64     `json:"mem_total_bytes" yaml:"mem_total_bytes"`
	TemperatureC      float64   `json:"temperature_c,omitempty" yaml:"temperature_c,omitempty"`
	TemperatureKnown  bool      `json:"temperature_known" yaml:"temperature_known"`
	ObservedAt        time.Time `json:"observed_at" yaml:"observed_at"`
}

// NodeEntry is one registered agent in the graph.
type NodeEntry struct {
	ID            string        `json:"id" yaml:"id"`
	Hostname      string        `json:"hostname" yaml:"hostname"`
	Virt          string        `json:"virt,omitempty" yaml:"virt,omitempty"`
	AgentVersion  string        `json:"agent_version,omitempty" yaml:"agent_version,omitempty"`
	SessionID     string        `json:"session_id,omitempty" yaml:"session_id,omitempty"`
	Status        Status        `json:"status" yaml:"status"`
	RegisteredAt  time.Time     `json:"registered_at" yaml:"registered_at"`
	LastHeartbeat time.Time     `json:"last_heartbeat" yaml:"last_heartbeat"`
	Telemetry     *Telemetry    `json:"telemetry,omitempty" yaml:"telemetry,omitempty"`
	Profile       *rdl.Document `json:"profile,omitempty" yaml:"profile,omitempty"`
}

// Snapshot is a YAML-serializable dump of the Resource Graph.
type Snapshot struct {
	GraphVersion string      `json:"graph_version" yaml:"graph_version"`
	GeneratedAt  time.Time   `json:"generated_at" yaml:"generated_at"`
	HeartbeatTO  string      `json:"heartbeat_timeout" yaml:"heartbeat_timeout"`
	Nodes        []NodeEntry `json:"nodes" yaml:"nodes"`
}

// Registry is a concurrent in-memory Resource Graph.
type Registry struct {
	mu               sync.RWMutex
	nodes            map[string]*NodeEntry
	heartbeatTimeout time.Duration
	now              func() time.Time
}

// New creates a registry. heartbeatTimeout marks nodes unavailable when exceeded.
func New(heartbeatTimeout time.Duration) *Registry {
	return NewWithClock(heartbeatTimeout, time.Now)
}

// NewWithClock is like New but injects the clock (tests).
func NewWithClock(heartbeatTimeout time.Duration, now func() time.Time) *Registry {
	if heartbeatTimeout <= 0 {
		heartbeatTimeout = 15 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &Registry{
		nodes:            map[string]*NodeEntry{},
		heartbeatTimeout: heartbeatTimeout,
		now:              now,
	}
}

// Register inserts or refreshes a node session.
func (r *Registry) Register(id, hostname, virt, agentVersion, sessionID string) *NodeEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now().UTC()
	n, ok := r.nodes[id]
	if !ok {
		n = &NodeEntry{
			ID:           id,
			RegisteredAt: now,
		}
		r.nodes[id] = n
	}
	n.Hostname = hostname
	n.Virt = virt
	n.AgentVersion = agentVersion
	n.SessionID = sessionID
	n.Status = StatusAvailable
	n.LastHeartbeat = now
	return cloneNode(n)
}

// Heartbeat refreshes liveness. Returns false if the node/session is unknown.
func (r *Registry) Heartbeat(id, sessionID string, tel *Telemetry) (ok bool, reAdvertise bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, exists := r.nodes[id]
	if !exists {
		return false, true
	}
	if sessionID != "" && n.SessionID != "" && sessionID != n.SessionID {
		return false, true
	}
	n.LastHeartbeat = r.now().UTC()
	n.Status = StatusAvailable
	if tel != nil {
		cp := *tel
		n.Telemetry = &cp
	}
	reAdvertise = n.Profile == nil
	return true, reAdvertise
}

// Advertise stores a validated Machine Profile for the node.
func (r *Registry) Advertise(id, sessionID string, doc *rdl.Document) error {
	if doc == nil {
		return fmt.Errorf("profile is nil")
	}
	if err := doc.Validate(); err != nil {
		return err
	}
	if doc.Node.ID != "" && doc.Node.ID != id {
		return fmt.Errorf("profile node.id %q does not match request node_id %q", doc.Node.ID, id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.nodes[id]
	if !ok {
		return fmt.Errorf("node %q is not registered", id)
	}
	if sessionID != "" && n.SessionID != "" && sessionID != n.SessionID {
		return fmt.Errorf("session mismatch for node %q", id)
	}
	cp := *doc
	n.Profile = &cp
	if n.Hostname == "" {
		n.Hostname = doc.Node.Hostname
	}
	if n.Virt == "" {
		n.Virt = doc.Node.Virt
	}
	n.LastHeartbeat = r.now().UTC()
	n.Status = StatusAvailable
	return nil
}

// RecordTelemetry stores a streamed sample.
func (r *Registry) RecordTelemetry(id, sessionID string, tel Telemetry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.nodes[id]
	if !ok {
		return fmt.Errorf("node %q is not registered", id)
	}
	if sessionID != "" && n.SessionID != "" && sessionID != n.SessionID {
		return fmt.Errorf("session mismatch for node %q", id)
	}
	cp := tel
	n.Telemetry = &cp
	n.LastHeartbeat = r.now().UTC()
	n.Status = StatusAvailable
	return nil
}

// Sweep marks stale nodes unavailable. Returns how many flipped.
func (r *Registry) Sweep() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now().UTC()
	flipped := 0
	for _, n := range r.nodes {
		if n.Status == StatusAvailable && now.Sub(n.LastHeartbeat) > r.heartbeatTimeout {
			n.Status = StatusUnavailable
			flipped++
		}
	}
	return flipped
}

// Counts returns total and available node counts after applying timeout.
func (r *Registry) Counts() (total, available int) {
	r.Sweep()
	r.mu.RLock()
	defer r.mu.RUnlock()
	total = len(r.nodes)
	for _, n := range r.nodes {
		if n.Status == StatusAvailable {
			available++
		}
	}
	return total, available
}

// Snapshot returns a stable, deep-copied graph dump.
func (r *Registry) Snapshot() Snapshot {
	r.Sweep()
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := Snapshot{
		GraphVersion: "0.2",
		GeneratedAt:  r.now().UTC(),
		HeartbeatTO:  r.heartbeatTimeout.String(),
		Nodes:        make([]NodeEntry, 0, len(r.nodes)),
	}
	ids := make([]string, 0, len(r.nodes))
	for id := range r.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out.Nodes = append(out.Nodes, *cloneNode(r.nodes[id]))
	}
	return out
}

// MarshalYAML encodes Snapshot as YAML.
func (s Snapshot) MarshalYAML() ([]byte, error) {
	return yaml.Marshal(s)
}

func cloneNode(n *NodeEntry) *NodeEntry {
	cp := *n
	if n.Telemetry != nil {
		tel := *n.Telemetry
		cp.Telemetry = &tel
	}
	if n.Profile != nil {
		doc := *n.Profile
		if n.Profile.Resources != nil {
			doc.Resources = append([]rdl.Resource{}, n.Profile.Resources...)
		}
		if n.Profile.Transports != nil {
			doc.Transports = append([]rdl.Transport{}, n.Profile.Transports...)
		}
		if n.Profile.Limits != nil {
			doc.Limits = append([]any{}, n.Profile.Limits...)
		}
		if n.Profile.Behavior != nil {
			doc.Behavior = append([]any{}, n.Profile.Behavior...)
		}
		if n.Profile.Node.Warnings != nil {
			doc.Node.Warnings = append([]string{}, n.Profile.Node.Warnings...)
		}
		cp.Profile = &doc
	}
	return &cp
}
