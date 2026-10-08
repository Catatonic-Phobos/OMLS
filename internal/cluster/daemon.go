package cluster

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/community"
	"github.com/Catatonic-Phobos/OMLS/internal/discover"
	"github.com/Catatonic-Phobos/OMLS/internal/fabric"
	"github.com/Catatonic-Phobos/OMLS/internal/graph"
)

// Config starts one OMLS node daemon.
type Config struct {
	DataDir          string
	Listen           string
	SocketPath       string
	Interval         time.Duration
	HeartbeatTimeout time.Duration
	Grace            time.Duration
	StableFor        time.Duration
	CommunityDir     string
	AdvertiseHost    string
	NodeID           string
	Directory        Directory
}

// Daemon is one resident OMLS node: discovery, membership, and a temporary
// coordinator role backed by the existing fabric and Resource Graph.
type Daemon struct {
	cfg       Config
	self      Member
	localDial string
	advHost   string
	advPort   int
	members   *Membership
	dir       Directory

	mu        sync.Mutex
	roleName  string
	coordID   string
	coordDial string
	reg       *graph.Registry
	role      *roleRun
}

type roleRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Run discovers hardware, publishes the node, and serves the local CLI socket
// until ctx is cancelled.
func Run(ctx context.Context, cfg Config) error {
	d, err := prepare(cfg)
	if err != nil {
		return err
	}
	return d.run(ctx)
}

func prepare(cfg Config) (*Daemon, error) {
	if cfg.Listen == "" {
		cfg.Listen = ":7443"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath = DefaultSocketPath()
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.HeartbeatTimeout <= 0 {
		cfg.HeartbeatTimeout = 15 * time.Second
	}
	if cfg.Grace <= 0 {
		cfg.Grace = 2 * time.Second
	}
	if cfg.StableFor < 0 {
		cfg.StableFor = 0
	}
	if cfg.StableFor == 0 && cfg.Grace > 0 {
		// A short hold after the grace period keeps a late browse result
		// from flipping the coordinator twice at boot.
		cfg.StableFor = 500 * time.Millisecond
	}
	port, err := listenPort(cfg.Listen)
	if err != nil {
		return nil, err
	}
	localDial, err := loopbackDial(cfg.Listen)
	if err != nil {
		return nil, err
	}
	res, err := discover.Discover(discover.Options{})
	if err != nil {
		return nil, err
	}
	doc := res.Document
	id, err := LoadOrCreate(cfg.DataDir, doc.Node.ID)
	if err != nil {
		return nil, err
	}
	if cfg.NodeID != "" && cfg.NodeID != id.NodeID {
		id.NodeID = cfg.NodeID
		if err := SaveIdentity(cfg.DataDir, id); err != nil {
			return nil, err
		}
	}
	host := cfg.AdvertiseHost
	if host == "" {
		host = AdvertiseHost()
	}
	self := Member{
		NodeID:   id.NodeID,
		Hostname: doc.Node.Hostname,
		OS:       doc.Node.OS.Pretty,
		Virt:     doc.Node.Virt,
		Addr:     PeerAddr(host, port),
		Status:   StatusReady,
	}
	dir := cfg.Directory
	if dir == nil {
		dir = NewMDNSDirectory()
	}
	fmt.Fprintf(os.Stderr, "omlsd: node_id=%s hostname=%s virt=%s listen=%s advertise=%s socket=%s\n",
		self.NodeID, self.Hostname, self.Virt, cfg.Listen, self.Addr, cfg.SocketPath)
	if self.Virt == "wsl" {
		fmt.Fprintf(os.Stderr, "omlsd: WSL detected; LAN peers are found with mDNS multicast, which needs mirrored networking to leave the WSL network\n")
	}
	return &Daemon{
		cfg:       cfg,
		self:      self,
		localDial: localDial,
		advHost:   host,
		advPort:   port,
		members:   NewMembership(cfg.HeartbeatTimeout, self),
		dir:       dir,
	}, nil
}

func (d *Daemon) run(ctx context.Context) error {
	apiErr := make(chan error, 1)
	go func() {
		apiErr <- d.serveAPI(ctx)
	}()

	events, err := d.dir.Watch(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "omlsd: discovery watch: %v\n", err)
	}
	peer := Peer{
		NodeID:          d.self.NodeID,
		Hostname:        d.self.Hostname,
		OS:              d.self.OS,
		Virt:            d.self.Virt,
		Host:            d.advHost,
		Port:            d.advPort,
		ProtocolVersion: ProtocolVersion,
	}
	if err := d.dir.Announce(ctx, peer); err != nil {
		fmt.Fprintf(os.Stderr, "omlsd: discovery announce: %v\n", err)
	}

	defer d.stopRole()

	electionReady := d.cfg.Grace <= 0
	var grace <-chan time.Time
	if !electionReady {
		timer := time.NewTimer(d.cfg.Grace)
		defer timer.Stop()
		grace = timer.C
	}
	var pending string
	var pendingSince time.Time
	sweep := time.NewTicker(time.Second)
	defer sweep.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-apiErr:
			if err != nil && ctx.Err() == nil {
				return err
			}
			return nil
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			d.applyEvent(ev)
		case <-grace:
			electionReady = true
			grace = nil
		case <-sweep.C:
			d.members.Sweep(time.Now())
		case <-tick.C:
		}
		if electionReady {
			d.maybeElect(ctx, &pending, &pendingSince)
		}
	}
}

func (d *Daemon) applyEvent(ev Event) {
	if ev.Peer.NodeID == "" || ev.Peer.NodeID == d.self.NodeID {
		return
	}
	if ev.Peer.ProtocolVersion != "" && ev.Peer.ProtocolVersion != ProtocolVersion {
		return
	}
	member := Member{
		NodeID:   ev.Peer.NodeID,
		Hostname: ev.Peer.Hostname,
		OS:       ev.Peer.OS,
		Virt:     ev.Peer.Virt,
		Addr:     PeerAddr(ev.Peer.Host, ev.Peer.Port),
		LastSeen: time.Now(),
	}
	switch ev.Kind {
	case EventLeave:
		d.members.MarkUnavailable(ev.Peer.NodeID)
	default:
		d.members.Upsert(member)
	}
}

func (d *Daemon) maybeElect(ctx context.Context, pending *string, since *time.Time) {
	coord, ok := d.members.Coordinator()
	if !ok {
		return
	}
	if coord.NodeID != *pending {
		*pending = coord.NodeID
		*since = time.Now()
		if d.cfg.StableFor > 0 {
			return
		}
	}
	if d.cfg.StableFor > 0 && time.Since(*since) < d.cfg.StableFor {
		return
	}
	d.mu.Lock()
	same := d.coordID == coord.NodeID && d.role != nil
	roleName := d.roleName
	dial := d.coordDial
	d.mu.Unlock()
	if same {
		if roleName == "member" && coord.Addr != "" && coord.Addr != dial {
			d.transition(ctx, coord)
		}
		return
	}
	d.transition(ctx, coord)
}

func (d *Daemon) transition(ctx context.Context, coord Member) {
	d.stopRole()
	if coord.NodeID == d.self.NodeID {
		d.startCoordinator(ctx)
		fmt.Fprintf(os.Stderr, "omlsd: role=coordinator id=%s\n", d.self.NodeID)
		return
	}
	d.startMember(ctx, coord)
	fmt.Fprintf(os.Stderr, "omlsd: role=member coordinator=%s addr=%s\n", coord.Hostname, coord.Addr)
}

func (d *Daemon) startCoordinator(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	reg := graph.New(d.cfg.HeartbeatTimeout)
	d.mu.Lock()
	d.role = &roleRun{cancel: cancel, done: done}
	d.roleName = "coordinator"
	d.coordID = d.self.NodeID
	d.coordDial = d.localDial
	d.reg = reg
	d.mu.Unlock()
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			d.serveFabric(ctx, reg)
		}()
		go func() {
			defer wg.Done()
			d.runAgent(ctx, d.localDial)
		}()
		wg.Wait()
	}()
}

func (d *Daemon) startMember(parent context.Context, coord Member) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	d.mu.Lock()
	d.role = &roleRun{cancel: cancel, done: done}
	d.roleName = "member"
	d.coordID = coord.NodeID
	d.coordDial = coord.Addr
	d.reg = nil
	d.mu.Unlock()
	go func() {
		defer close(done)
		d.runAgent(ctx, coord.Addr)
	}()
}

func (d *Daemon) stopRole() {
	d.mu.Lock()
	role := d.role
	d.role = nil
	d.roleName = ""
	d.coordID = ""
	d.coordDial = ""
	d.reg = nil
	d.mu.Unlock()
	if role == nil {
		return
	}
	role.cancel()
	<-role.done
}

func (d *Daemon) serveFabric(ctx context.Context, reg *graph.Registry) {
	communityDir := d.cfg.CommunityDir
	if communityDir == "" {
		communityDir = community.ResolveDir("")
	}
	for {
		if ctx.Err() != nil {
			return
		}
		err := fabric.ListenAndServe(ctx, fabric.ServeOptions{
			Listen:       d.cfg.Listen,
			Registry:     reg,
			Interval:     d.cfg.Interval,
			DataDir:      d.cfg.DataDir,
			CommunityDir: communityDir,
		})
		if ctx.Err() != nil || err == nil {
			return
		}
		fmt.Fprintf(os.Stderr, "omlsd: fabric listen: %v\n", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (d *Daemon) runAgent(ctx context.Context, addr string) {
	for {
		if ctx.Err() != nil {
			return
		}
		conn, client, err := fabric.Dial(ctx, fabric.DialOptions{Addr: addr, Timeout: 3 * time.Second})
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}
		err = fabric.Run(ctx, fabric.AgentConfig{
			Client:       client,
			Conn:         conn,
			Interval:     d.cfg.Interval,
			AgentVersion: fabric.Version,
			NodeID:       d.self.NodeID,
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "omlsd: fabric agent: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
