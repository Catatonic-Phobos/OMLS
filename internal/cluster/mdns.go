package cluster

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	// ServiceType is the DNS-SD type advertised on the LAN.
	ServiceType = "_omls._tcp"
	// ProtocolVersion is the cluster discovery version carried in TXT records.
	ProtocolVersion = "1"
	mdnsDomain      = "local"
)

// MDNSDirectory publishes and browses _omls._tcp.local.
type MDNSDirectory struct {
	// Interval is how often a fresh browse reconciles who is still present.
	// The underlying browser reports each instance once per query, so presence
	// is rechecked on this cadence.
	Interval time.Duration
}

// NewMDNSDirectory uses a 5s browse cadence.
func NewMDNSDirectory() *MDNSDirectory {
	return &MDNSDirectory{Interval: 5 * time.Second}
}

// Announce registers this node until ctx is cancelled.
func (d *MDNSDirectory) Announce(ctx context.Context, self Peer) error {
	if self.Port <= 0 {
		return fmt.Errorf("mdns announce: port is required")
	}
	if self.NodeID == "" {
		return fmt.Errorf("mdns announce: node_id is required")
	}
	txt := []string{
		"node_id=" + self.NodeID,
		"proto=" + ProtocolVersion,
		"hostname=" + self.Hostname,
		"os=" + self.OS,
		"virt=" + self.Virt,
		"addr=" + self.Host,
		// Discovery only. Execute/schedule require a fabric join; do not advertise them here.
		"cap=discover",
	}
	server, err := zeroconf.Register(self.NodeID, ServiceType, "local.", self.Port, txt, nil)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		server.Shutdown()
	}()
	return nil
}

// Watch emits join, refresh, and leave events by browsing periodically.
func (d *MDNSDirectory) Watch(ctx context.Context) (<-chan Event, error) {
	interval := d.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ch := make(chan Event, 32)
	go func() {
		defer close(ch)
		known := map[string]Peer{}
		missing := map[string]int{}
		browse := func() {
			found, err := browseOnce(ctx, interval)
			if err != nil || ctx.Err() != nil {
				return
			}
			seen := map[string]bool{}
			for _, p := range found {
				if p.NodeID == "" || p.ProtocolVersion != ProtocolVersion {
					continue
				}
				seen[p.NodeID] = true
				delete(missing, p.NodeID)
				prev, ok := known[p.NodeID]
				known[p.NodeID] = p
				kind := EventUpdate
				if !ok || prev.Host != p.Host || prev.Port != p.Port {
					kind = EventJoin
				}
				offer(ch, Event{Kind: kind, Peer: p})
			}
			for id, prev := range known {
				if seen[id] {
					continue
				}
				missing[id]++
				// Two consecutive misses avoid a single empty browse marking
				// a live peer unavailable.
				if missing[id] >= 2 {
					offer(ch, Event{Kind: EventLeave, Peer: prev})
					delete(known, id)
					delete(missing, id)
				}
			}
		}
		browse()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				browse()
			}
		}
	}()
	return ch, nil
}

func browseOnce(ctx context.Context, wait time.Duration) ([]Peer, error) {
	if wait <= 0 {
		wait = 2 * time.Second
	}
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, err
	}
	entries := make(chan *zeroconf.ServiceEntry, 16)
	bctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- resolver.Browse(bctx, ServiceType, mdnsDomain, entries)
	}()
	var peers []Peer
	seen := map[string]bool{}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return peers, ctx.Err()
		case <-timer.C:
			return peers, nil
		case e, ok := <-entries:
			if !ok {
				return peers, nil
			}
			p, ok := peerFromEntry(e)
			if !ok || seen[p.NodeID] {
				continue
			}
			seen[p.NodeID] = true
			peers = append(peers, p)
		case <-errCh:
			// Browse returns when the timeout context ends. Keep draining
			// briefly so a late entry is not lost, then stop.
			timer.Reset(50 * time.Millisecond)
		}
	}
}

func peerFromEntry(e *zeroconf.ServiceEntry) (Peer, bool) {
	if e == nil {
		return Peer{}, false
	}
	txt := parseTXT(e.Text)
	id := txt["node_id"]
	if id == "" {
		id = e.Instance
	}
	if id == "" {
		return Peer{}, false
	}
	host := txt["addr"]
	if host == "" {
		host = firstIPv4(e.AddrIPv4)
	}
	if host == "" {
		return Peer{}, false
	}
	proto := txt["proto"]
	if proto == "" {
		proto = ProtocolVersion
	}
	return Peer{
		NodeID:          id,
		Hostname:        txt["hostname"],
		OS:              txt["os"],
		Virt:            txt["virt"],
		Host:            host,
		Port:            e.Port,
		ProtocolVersion: proto,
	}, true
}

func parseTXT(text []string) map[string]string {
	out := make(map[string]string, len(text))
	for _, item := range text {
		k, v, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		out[k] = v
	}
	return out
}

func firstIPv4(ips []net.IP) string {
	var fallback string
	for _, ip := range ips {
		v4 := ip.To4()
		if v4 == nil || v4.IsUnspecified() {
			continue
		}
		if v4.IsLoopback() {
			if fallback == "" {
				fallback = v4.String()
			}
			continue
		}
		return v4.String()
	}
	return fallback
}

// AdvertiseHost picks a LAN IPv4 address other nodes can dial.
// Loopback is used only when the machine has no other IPv4 address.
func AdvertiseHost() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	var linkLocal string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := ipnet.IP.To4()
			if v4 == nil || v4.IsLoopback() || v4.IsUnspecified() {
				continue
			}
			if v4.IsLinkLocalUnicast() {
				if linkLocal == "" {
					linkLocal = v4.String()
				}
				continue
			}
			return v4.String()
		}
	}
	if linkLocal != "" {
		return linkLocal
	}
	return "127.0.0.1"
}

func listenPort(listen string) (int, error) {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return 0, fmt.Errorf("listen address %q: %w", listen, err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("listen address %q: invalid port", listen)
	}
	return n, nil
}

func loopbackDial(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("listen address %q: %w", listen, err)
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}
