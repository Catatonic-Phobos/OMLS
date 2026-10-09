package cluster

import (
	"context"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDaemonsDiscoverAndFailOver(t *testing.T) {
	bus := NewMemoryDirectory()
	listen := freeListen(t)

	stopA, sockA := startNode(t, bus, "a-node", listen)
	stopB, sockB := startNode(t, bus, "b-node", listen)
	defer stopB()

	waitStatus(t, sockA, func(s Status) bool {
		return s.Role == "coordinator" && s.Nodes == 2 && s.CoordinatorID == "a-node"
	})
	waitStatus(t, sockB, func(s Status) bool {
		return s.Role == "member" && s.Nodes == 2 && s.CoordinatorID == "a-node"
	})
	waitNodes(t, sockB, func(nodes []NodeInfo) bool {
		return hasNode(nodes, "a-node", StatusJoined) && hasNode(nodes, "b-node", StatusJoined)
	})

	waitGraph(t, sockA, "a-node", "b-node")

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var demo DemoResult
	err := PostJSON(ctx, sockB, "/v1/run-demo", DemoRequest{
		Workers: 4, Iterations: 1, WorkIterations: 1000, Policy: "place", TimeoutMs: 30_000,
	}, &demo)
	if err != nil {
		t.Fatal(err)
	}
	if len(demo.Rounds) == 0 {
		t.Fatal("run-demo returned no rounds")
	}
	round := demo.Rounds[0]
	if round.PlannedHosts < 2 {
		t.Fatalf("planned_hosts=%d, want >= 2 distinct computers", round.PlannedHosts)
	}
	if round.ExecutedHosts < 2 {
		t.Fatalf("executed_hosts=%d summary=%q — work did not run on multiple machines", round.ExecutedHosts, demo.Summary)
	}
	funcs := map[string]string{}
	for _, a := range round.Allocations {
		if a.Executed < 1 {
			t.Fatalf("node %s planned=%d executed=%d — allocation without execution", a.NodeID, a.Workers, a.Executed)
		}
		if a.Function != "" {
			if other, ok := funcs[a.Function]; ok && other != a.NodeID {
				t.Fatalf("function %s on both %s and %s", a.Function, other, a.NodeID)
			}
			funcs[a.Function] = a.NodeID
		}
	}
	if len(funcs) < 2 {
		t.Fatalf("expected distinct functions on distinct nodes, got %v", funcs)
	}
	hosts := map[string]bool{}
	for _, id := range funcs {
		hosts[id] = true
	}
	if len(hosts) < 2 {
		t.Fatalf("functions were not separated across computers: %v", funcs)
	}

	stopA()
	waitNodes(t, sockB, func(nodes []NodeInfo) bool {
		return hasNode(nodes, "a-node", StatusUnavailable) && hasNode(nodes, "b-node", StatusJoined)
	})
	waitStatus(t, sockB, func(s Status) bool {
		return s.Role == "coordinator" && s.CoordinatorID == "b-node"
	})
}

func TestBootOrderDoesNotChangeCluster(t *testing.T) {
	bus := NewMemoryDirectory()
	listen := freeListen(t)

	stopB, sockB := startNode(t, bus, "b-node", listen)
	defer stopB()
	waitStatus(t, sockB, func(s Status) bool {
		return s.Role == "coordinator" && s.CoordinatorID == "b-node"
	})

	stopA, sockA := startNode(t, bus, "a-node", listen)
	defer stopA()
	waitStatus(t, sockA, func(s Status) bool {
		return s.Role == "coordinator" && s.CoordinatorID == "a-node" && s.Nodes == 2
	})
	waitStatus(t, sockB, func(s Status) bool {
		return s.Role == "member" && s.CoordinatorID == "a-node" && s.Nodes == 2
	})
}

func startNode(t *testing.T, bus *MemoryDirectory, id, listen string) (func(), string) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "omlsd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Config{
			DataDir:          dir,
			Listen:           listen,
			SocketPath:       sock,
			Interval:         200 * time.Millisecond,
			HeartbeatTimeout: time.Minute,
			Grace:            100 * time.Millisecond,
			StableFor:        80 * time.Millisecond,
			AdvertiseHost:    "127.0.0.1",
			NodeID:           id,
			Directory:        bus,
			CommunityDir:     t.TempDir(),
			CacheListen:      "-",
		})
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-errCh:
				if err != nil {
					t.Errorf("daemon %s: %v", id, err)
				}
			case <-time.After(15 * time.Second):
				t.Errorf("daemon %s did not stop", id)
			}
		})
	}
	t.Cleanup(stop)
	return stop, sock
}

func freeListen(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func waitStatus(t *testing.T, sock string, want func(Status) bool) Status {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last Status
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := GetJSON(ctx, sock, "/v1/status", &last)
		cancel()
		if err == nil && want(last) {
			return last
		}
		lastErr = err
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("status timeout sock=%s last=%+v err=%v", sock, last, lastErr)
	return last
}

func waitNodes(t *testing.T, sock string, want func([]NodeInfo) bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last []NodeInfo
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := GetJSON(ctx, sock, "/v1/nodes", &last)
		cancel()
		if err == nil && want(last) {
			return
		}
		lastErr = err
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("nodes timeout last=%+v err=%v", last, lastErr)
}

func waitGraph(t *testing.T, sock string, ids ...string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last string
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		raw, err := GetBytes(ctx, sock, "/v1/graph")
		cancel()
		last = string(raw)
		lastErr = err
		if err == nil {
			ok := true
			for _, id := range ids {
				if !contains(last, id) {
					ok = false
					break
				}
			}
			if ok {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("graph timeout err=%v body=%s", lastErr, last)
}

func hasNode(nodes []NodeInfo, id, status string) bool {
	for _, n := range nodes {
		if n.NodeID == id && n.Status == status {
			return true
		}
	}
	return false
}
