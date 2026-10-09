package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/cache"
	"github.com/Catatonic-Phobos/OMLS/internal/cluster"
)

func runCache(args []string) error {
	if len(args) < 1 {
		fmt.Print(cacheUsage())
		return nil
	}
	switch args[0] {
	case "serve":
		return runCacheServe(args[1:])
	case "place":
		return runCachePlace(args[1:])
	case "status":
		return runCacheStatus(args[1:])
	case "help", "-h", "--help":
		fmt.Print(cacheUsage())
		return nil
	default:
		return fmt.Errorf("unknown cache subcommand %q", args[0])
	}
}

func runCacheServe(args []string) error {
	addr := cache.DefaultListen
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--listen":
			i++
			if i >= len(args) {
				return fmt.Errorf("--listen requires an address")
			}
			addr = args[i]
		case strings.HasPrefix(a, "--listen="):
			addr = strings.TrimPrefix(a, "--listen=")
		case a == "-h" || a == "--help":
			fmt.Print(cacheUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(os.Stderr, "omls cache: listening %s\n", addr)
	return cache.Serve(ctx, addr, cache.NewStore())
}

func runCachePlace(args []string) error {
	node := ""
	peer := ""
	reserve := cache.DefaultReserve
	fill := cache.DefaultFill
	kind := cache.KindMemory
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--node":
			i++
			if i >= len(args) {
				return fmt.Errorf("--node requires a hostname")
			}
			node = args[i]
		case strings.HasPrefix(a, "--node="):
			node = strings.TrimPrefix(a, "--node=")
		case a == "--peer":
			i++
			if i >= len(args) {
				return fmt.Errorf("--peer requires host:port")
			}
			peer = args[i]
		case strings.HasPrefix(a, "--peer="):
			peer = strings.TrimPrefix(a, "--peer=")
		case a == "--bytes":
			i++
			if i >= len(args) {
				return fmt.Errorf("--bytes requires a size")
			}
			n, err := parseSize(args[i])
			if err != nil {
				return err
			}
			reserve = n
		case strings.HasPrefix(a, "--bytes="):
			n, err := parseSize(strings.TrimPrefix(a, "--bytes="))
			if err != nil {
				return err
			}
			reserve = n
		case a == "--fill":
			i++
			if i >= len(args) {
				return fmt.Errorf("--fill requires a size")
			}
			n, err := parseSize(args[i])
			if err != nil {
				return err
			}
			fill = n
		case strings.HasPrefix(a, "--fill="):
			n, err := parseSize(strings.TrimPrefix(a, "--fill="))
			if err != nil {
				return err
			}
			fill = n
		case a == "--kind":
			i++
			if i >= len(args) {
				return fmt.Errorf("--kind requires memory or graphics")
			}
			kind = args[i]
		case strings.HasPrefix(a, "--kind="):
			kind = strings.TrimPrefix(a, "--kind=")
		case a == "-h" || a == "--help":
			fmt.Print(cacheUsage())
			return nil
		default:
			return fmt.Errorf("unknown flag %q", a)
		}
	}
	if fill > reserve {
		fill = reserve
	}
	var label string
	if peer == "" {
		var err error
		label, peer, err = pickPeer(node)
		if err != nil {
			return err
		}
	} else {
		label = peer
	}
	sl, err := cache.ReadSlice(kind, fill)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	st, err := cache.Place(ctx, peer, reserve, sl)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	fmt.Printf("node: %s\nkind: %s\nreserved: %s\nfilled: %s\nlocked: %t\nsource: %s\nverified: yes\n",
		label, st.Kind, formatBytes(st.Capacity), formatBytes(st.Used), st.Locked, st.Source)
	return nil
}

func runCacheStatus(args []string) error {
	if helpRequested(args) {
		fmt.Print(cacheUsage())
		return nil
	}
	peers, err := peerList()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fmt.Printf("%-16s %-10s %-12s %-12s %s\n", "NODE", "KIND", "RESERVED", "FILLED", "SOURCE")
	for _, p := range peers {
		st, err := cache.FetchStat(ctx, p.cache)
		if err != nil {
			fmt.Printf("%-16s %-10s %-12s %-12s %s\n", clip(p.name, 16), "-", "-", "-", "sem reserva")
			continue
		}
		if st.Capacity == 0 {
			fmt.Printf("%-16s %-10s %-12s %-12s %s\n", clip(p.name, 16), "-", "0", "0", "sem reserva")
			continue
		}
		fmt.Printf("%-16s %-10s %-12s %-12s %s\n",
			clip(p.name, 16), st.Kind, formatBytes(st.Capacity), formatBytes(st.Used), st.Source)
	}
	return nil
}

type peerAddr struct {
	name  string
	cache string
}

func pickPeer(want string) (string, string, error) {
	peers, err := peerList()
	if err != nil {
		return "", "", err
	}
	self, _ := os.Hostname()
	var chosen *peerAddr
	for i := range peers {
		p := &peers[i]
		if want != "" {
			if strings.EqualFold(p.name, want) {
				chosen = p
				break
			}
			continue
		}
		if strings.EqualFold(p.name, self) {
			continue
		}
		if strings.EqualFold(p.name, "omls") || chosen == nil {
			chosen = p
		}
	}
	if chosen == nil {
		return "", "", fmt.Errorf("no other node is ready")
	}
	return chosen.name, chosen.cache, nil
}

func peerList() ([]peerAddr, error) {
	sock, err := cluster.ResolveSocket("")
	if err != nil {
		return nil, daemonClientErr(err)
	}
	var nodes []cluster.NodeInfo
	if err := cluster.GetJSON(context.Background(), sock, "/v1/nodes", &nodes); err != nil {
		return nil, daemonClientErr(err)
	}
	var out []peerAddr
	for _, n := range nodes {
		if n.Status != "joined" && n.Status != "ready" && n.Status != cluster.StatusReady {
			continue
		}
		if n.Addr == "" {
			continue
		}
		name := n.Name
		if name == "" {
			name = n.NodeID
		}
		out = append(out, peerAddr{name: name, cache: cache.CachePort(n.Addr)})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no joined node has an address")
	}
	return out, nil
}

func parseSize(s string) (int, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return 0, fmt.Errorf("empty size")
	}
	mult := 1
	upper := strings.ToUpper(raw)
	switch {
	case strings.HasSuffix(upper, "GIB"), strings.HasSuffix(upper, "GB"), strings.HasSuffix(upper, "G"):
		mult = 1 << 30
		upper = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(upper, "GIB"), "GB"), "G")
	case strings.HasSuffix(upper, "MIB"), strings.HasSuffix(upper, "MB"), strings.HasSuffix(upper, "M"):
		mult = 1 << 20
		upper = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(upper, "MIB"), "MB"), "M")
	case strings.HasSuffix(upper, "KIB"), strings.HasSuffix(upper, "KB"), strings.HasSuffix(upper, "K"):
		mult = 1 << 10
		upper = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(upper, "KIB"), "KB"), "K")
	}
	n, err := strconv.Atoi(strings.TrimSpace(upper))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	if n > (1<<31-1)/mult {
		return 0, fmt.Errorf("size %q is too large", s)
	}
	return n * mult, nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func formatBytes(n int) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func cacheUsage() string {
	return `omls cache — RAM reservation on another node (1.3)

Usage:
  omls cache serve [--listen :7444]
  omls cache place [--node NAME] [--peer HOST:7444] [--bytes 512MB] [--fill 32MB] [--kind memory|graphics]
  omls cache status

place reserves RAM on the other node and copies a slice into it.
--kind memory copies resident pages of The Sims 4.
--kind graphics copies the game window.
The reservation stays in the peer process until the cache server stops.
`
}
