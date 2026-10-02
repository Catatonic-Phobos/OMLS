package fabric_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/behavior"
	"github.com/Catatonic-Phobos/OMLS/internal/fabric"
	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
	omlsv1 "github.com/Catatonic-Phobos/OMLS/proto/omls/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestRunDemoTwoSimulatedWorkers(t *testing.T) {
	reg := graph.New(time.Minute)
	store, err := behavior.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := fabric.NewServerWithStore(reg, time.Second, store)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	omlsv1.RegisterFabricServer(gs, srv)
	go gs.Serve(lis) //nolint:errcheck
	defer gs.Stop()

	// Register two unequal nodes directly (simulate heterogeneous agents).
	seedNode(reg, "node-fast", "fast-host", "kvm", 8)
	seedNode(reg, "node-slow", "slow-host", "wsl", 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, client, err := fabric.Dial(ctx, fabric.DialOptions{
		Addr: lis.Addr().String(), TLS: insecure.NewCredentials(), Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Simulated agents: claim/report with fixed durations (fast vs slow).
	go fakeWorker(ctx, t, client, "node-fast", 20)
	go fakeWorker(ctx, t, client, "node-slow", 80)

	resp, err := client.RunDemo(ctx, &omlsv1.RunDemoRequest{
		Workers:        8,
		Iterations:     3,
		WorkIterations: 1000, // ignored by fake workers
		TimeoutMs:      15000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Rounds) != 3 {
		t.Fatalf("rounds=%d", len(resp.Rounds))
	}
	last := resp.Rounds[len(resp.Rounds)-1]
	fastW, slowW := 0, 0
	for _, a := range last.Allocations {
		switch a.NodeId {
		case "node-fast":
			fastW = int(a.Workers)
		case "node-slow":
			slowW = int(a.Workers)
		}
	}
	if fastW <= slowW {
		t.Fatalf("expected learning to prefer fast node: last=%v summary=%s", last.Allocations, resp.Summary)
	}
}

func seedNode(reg *graph.Registry, id, host, virt string, cpus int) {
	reg.Register(id, host, virt, "test", "sess-"+id)
	doc := rdl.Empty()
	doc.Node = rdl.Node{ID: id, Hostname: host, OS: rdl.OSInfo{Family: "linux"}, Virt: virt}
	doc.Resources = []rdl.Resource{{
		ID: "cpu0", Kind: "device", Class: "compute",
		Capabilities: []string{"compute", "parallelizable"},
		Attrs:        map[string]any{"logical_cpus": cpus},
	}}
	_ = reg.Advertise(id, "sess-"+id, &doc)
	_, _ = reg.Heartbeat(id, "sess-"+id, &graph.Telemetry{CPULoad: 0.1, ObservedAt: time.Now()})
}

func fakeWorker(ctx context.Context, t *testing.T, client omlsv1.FabricClient, nodeID string, durationMs int64) {
	t.Helper()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		claim, err := client.ClaimWork(ctx, &omlsv1.ClaimWorkRequest{NodeId: nodeID, SessionId: "sess-" + nodeID})
		if err != nil {
			// server stopping
			return
		}
		if !claim.GetHasWork() {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		_, _ = client.ReportWork(ctx, &omlsv1.ReportWorkRequest{
			NodeId:     nodeID,
			SessionId:  "sess-" + nodeID,
			WorkId:     claim.GetUnit().GetWorkId(),
			DurationMs: durationMs,
		})
	}
}
