package fabric_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/behavior"
	"github.com/Catatonic-Phobos/OMLS/internal/fabric"
	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	omlsv1 "github.com/Catatonic-Phobos/OMLS/proto/omls/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestHeartbeatPersistsBehaviorProfile(t *testing.T) {
	dir := t.TempDir()
	store, err := behavior.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := graph.New(time.Minute)
	srv := fabric.NewServerWithStore(reg, time.Second, store)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	omlsv1.RegisterFabricServer(gs, srv)
	go gs.Serve(lis) //nolint:errcheck
	defer gs.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, client, err := fabric.Dial(ctx, fabric.DialOptions{
		Addr: lis.Addr().String(), TLS: insecure.NewCredentials(), Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	regResp, err := client.Register(ctx, &omlsv1.RegisterRequest{
		NodeId: "node-bp", Hostname: "bp-host", Virt: "kvm", AgentVersion: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Heartbeat(ctx, &omlsv1.HeartbeatRequest{
		NodeId: "node-bp", SessionId: regResp.GetSessionId(),
		Telemetry: &omlsv1.TelemetrySnapshot{
			CpuLoad: 0.8, MemAvailableBytes: 1 << 30, MemTotalBytes: 2 << 30,
			TemperatureC: 48, TemperatureKnown: true, ObservedUnixMs: time.Now().UnixMilli(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	list, err := client.ListProfiles(ctx, &omlsv1.ListProfilesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if list.GetDataDir() != dir {
		t.Fatalf("data_dir=%s want %s", list.GetDataDir(), dir)
	}
	if len(list.GetProfiles()) != 1 || list.Profiles[0].GetNodeId() != "node-bp" {
		t.Fatalf("%+v", list.GetProfiles())
	}
	if list.Profiles[0].GetHostname() != "bp-host" {
		t.Fatalf("%+v", list.Profiles[0])
	}

	got, err := client.GetProfile(ctx, &omlsv1.GetProfileRequest{NodeId: "node-bp", TelemetryTail: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.GetProfileYaml()) == 0 {
		t.Fatal("empty profile yaml")
	}
	if len(got.GetTelemetryJsonl()) == 0 {
		t.Fatal("expected telemetry jsonl")
	}

	// Direct store check
	p, err := store.Load("node-bp")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.RecentTelemetry) != 1 {
		t.Fatalf("ring=%d", len(p.RecentTelemetry))
	}
}

func TestRunDemoPersistsObserveWork(t *testing.T) {
	dir := t.TempDir()
	store, err := behavior.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := graph.New(time.Minute)
	srv := fabric.NewServerWithStore(reg, time.Second, store)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	omlsv1.RegisterFabricServer(gs, srv)
	go gs.Serve(lis) //nolint:errcheck
	defer gs.Stop()

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

	go fakeWorker(ctx, t, client, "node-fast", 20)
	go fakeWorker(ctx, t, client, "node-slow", 80)

	resp, err := client.RunDemo(ctx, &omlsv1.RunDemoRequest{
		Workers: 8, Iterations: 2, WorkIterations: 1000, TimeoutMs: 15000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Rounds) != 2 {
		t.Fatalf("rounds=%d", len(resp.Rounds))
	}

	list, err := client.ListProfiles(ctx, &omlsv1.ListProfilesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.GetProfiles()) < 2 {
		t.Fatalf("profiles=%d", len(list.GetProfiles()))
	}
	found := map[string]bool{}
	for _, p := range list.GetProfiles() {
		found[p.GetNodeId()] = true
		if p.GetSamples() < 1 {
			t.Fatalf("expected samples for %s: %+v", p.GetNodeId(), p)
		}
	}
	if !found["node-fast"] || !found["node-slow"] {
		t.Fatalf("%+v", list.GetProfiles())
	}

	got, err := client.GetProfile(ctx, &omlsv1.GetProfileRequest{NodeId: "node-fast"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.GetProfileYaml()) == 0 {
		t.Fatal("empty profile")
	}

	ewma, samples, ok := store.DurationEWMA("node-fast")
	if !ok || samples < 1 || ewma <= 0 {
		t.Fatalf("ewma=%v samples=%d ok=%v", ewma, samples, ok)
	}
}
