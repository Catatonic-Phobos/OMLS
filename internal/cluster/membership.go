package cluster

import (
	"net"
	"sort"
	"sync"
	"time"
)

const (
	// StatusReady means the peer is live on the LAN (mDNS / membership).
	// This is NOT proof that the peer joined the fabric or executes work.
	StatusReady = "ready"
	// StatusDiscovered is the operator-facing label for LAN presence without fabric join.
	StatusDiscovered = "discovered"
	// StatusJoined means the peer registered with the coordinator fabric and can claim work.
	StatusJoined = "joined"
	// StatusUnavailable is a member that disappeared and may return.
	StatusUnavailable = "unavailable"
)

// Member is one machine in the cluster membership view.
type Member struct {
	NodeID   string
	Hostname string
	OS       string
	Virt     string
	Addr     string
	LastSeen time.Time
	Status   string
	Self     bool
}

// Membership tracks peers discovered on the LAN.
// Records are kept after a peer disappears so operators can see "unavailable".
type Membership struct {
	mu      sync.Mutex
	timeout time.Duration
	self    Member
	peers   map[string]*Member
}

// NewMembership starts with the local node already ready.
func NewMembership(timeout time.Duration, self Member) *Membership {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	self.Status = StatusReady
	self.Self = true
	self.LastSeen = time.Now()
	return &Membership{
		timeout: timeout,
		self:    self,
		peers:   map[string]*Member{},
	}
}

// SetSelf refreshes the local member (address or hostname may change).
func (m *Membership) SetSelf(self Member) {
	m.mu.Lock()
	defer m.mu.Unlock()
	self.Status = StatusReady
	self.Self = true
	if self.LastSeen.IsZero() {
		self.LastSeen = time.Now()
	}
	m.self = self
}

// Upsert records a live peer. The same node_id rejoins as the same member.
func (m *Membership) Upsert(peer Member) {
	if peer.NodeID == "" || peer.NodeID == m.selfID() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.peers[peer.NodeID]
	if cur == nil {
		cur = &Member{NodeID: peer.NodeID}
		m.peers[peer.NodeID] = cur
	}
	cur.Hostname = peer.Hostname
	cur.OS = peer.OS
	cur.Virt = peer.Virt
	if peer.Addr != "" {
		cur.Addr = peer.Addr
	}
	cur.Status = StatusReady
	if peer.LastSeen.IsZero() {
		cur.LastSeen = time.Now()
	} else {
		cur.LastSeen = peer.LastSeen
	}
}

func (m *Membership) selfID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.self.NodeID
}

// MarkUnavailable keeps the member but shows it as down.
func (m *Membership) MarkUnavailable(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if nodeID == "" || nodeID == m.self.NodeID {
		return
	}
	if cur := m.peers[nodeID]; cur != nil {
		cur.Status = StatusUnavailable
	}
}

// Sweep marks peers unavailable when their last observation is too old.
func (m *Membership) Sweep(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, peer := range m.peers {
		if peer.Status == StatusReady && now.Sub(peer.LastSeen) > m.timeout {
			peer.Status = StatusUnavailable
		}
	}
}

// Snapshot returns the local node and every remembered peer.
func (m *Membership) Snapshot() []Member {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Member, 0, len(m.peers)+1)
	out = append(out, m.self)
	for _, peer := range m.peers {
		out = append(out, *peer)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hostname != out[j].Hostname {
			return out[i].Hostname < out[j].Hostname
		}
		return out[i].NodeID < out[j].NodeID
	})
	return out
}

// Coordinator picks the ready member with the lowest node_id.
// Every node with the same membership view elects the same coordinator.
// The role is temporary: if that node becomes unavailable, the next id wins.
func (m *Membership) Coordinator() (Member, bool) {
	snap := m.Snapshot()
	var best *Member
	for i := range snap {
		if snap[i].Status != StatusReady || snap[i].NodeID == "" {
			continue
		}
		if best == nil || snap[i].NodeID < best.NodeID {
			cp := snap[i]
			best = &cp
		}
	}
	if best == nil {
		return Member{}, false
	}
	return *best, true
}

// PeerAddr joins host and port for fabric dialing.
func PeerAddr(host string, port int) string {
	return net.JoinHostPort(host, itoa(port))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
