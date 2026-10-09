package fabric_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/behavior"
	"github.com/Catatonic-Phobos/OMLS/internal/community"
	"github.com/Catatonic-Phobos/OMLS/internal/envelope"
	"github.com/Catatonic-Phobos/OMLS/internal/fabric"
	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/learn"
	"github.com/Catatonic-Phobos/OMLS/internal/power"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
	"github.com/Catatonic-Phobos/OMLS/internal/sandbox"
	omlsv1 "github.com/Catatonic-Phobos/OMLS/proto/omls/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"
)

// TestStackSmoke1_0 exercises the cumulative 0.1→0.9 stack in-process:
// discover-like RDL → fabric → schedule → envelope → behavior → adaptive →
// community prior → sandbox resource → power budget. No multi-host required.
func TestStackSmoke1_0(t *testing.T) {
	if fabric.Version != "1.3.0" {
		t.Fatalf("fabric.Version=%s want 1.3.0", fabric.Version)
	}

	dir := t.TempDir()
	catDir := filepath.Join(dir, "community")
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(catDir, 0o755); err != nil {
		t.Fatal(err)
	}
	communityYAML := []byte(`
community_version: "0.7"
id: smoke-kvm
provenance:
  confidence: community_tested
  confidence_score: 0.6
match:
  arch: [x86_64]
  virt: [kvm]
priors:
  duration_ewma_ms: 50
  temp_delta_ewma: 1
  samples: 5
`)
	if err := os.WriteFile(filepath.Join(catDir, "smoke-kvm.yaml"), communityYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := community.Open(catDir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := behavior.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	reg := graph.New(time.Minute)
	srv := fabric.NewServerFull(reg, time.Second, store, cat)
	if srv.Power() == nil {
		t.Fatal("power simulator missing")
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	omlsv1.RegisterFabricServer(gs, srv)
	go gs.Serve(lis) //nolint:errcheck
	defer gs.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// --- 0.1/0.2: register + advertise Machine Profiles (with sandbox merge) ---
	for _, n := range []struct {
		id, host, virt string
		cpus           int
	}{
		{"node-fast", "fast-host", "kvm", 8},
		{"node-slow", "slow-host", "wsl", 2},
	} {
		rr, err := srv.Register(ctx, &omlsv1.RegisterRequest{
			NodeId: n.id, Hostname: n.host, Virt: n.virt, AgentVersion: fabric.Version,
		})
		if err != nil {
			t.Fatal(err)
		}
		doc := rdl.Empty()
		doc.Node.ID = n.id
		doc.Node.Hostname = n.host
		doc.Node.Virt = n.virt
		doc.Node.OS.Family = "linux"
		doc.Node.OS.Arch = "x86_64"
		doc.Resources = []rdl.Resource{{
			ID: "cpu0", Kind: "device", Class: "compute",
			Capabilities: []string{"compute", "parallelizable"},
			Attrs:        map[string]any{"arch": "x86_64", "logical_cpus": n.cpus, "cores": n.cpus},
		}}
		// 0.8: merge sandbox resources from a fixture probe
		sysRoot := filepath.Join(dir, "sys-"+n.id)
		_ = os.MkdirAll(filepath.Join(sysRoot, "sys/class/vfio"), 0o755)
		probe := sandbox.Probe(sandbox.Options{SysRoot: sysRoot})
		sandbox.MergeIntoDocument(&doc, probe)
		raw, _ := yaml.Marshal(doc)
		if _, err := srv.AdvertiseProfile(ctx, &omlsv1.AdvertiseProfileRequest{
			NodeId: n.id, SessionId: rr.SessionId, Format: "yaml", Profile: raw,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// 0.7: community prior should have seeded node-fast (kvm match, no local samples yet)
	if dur, samples, ok := store.DurationEWMA("node-fast"); !ok || samples != 5 || dur != 50 {
		t.Fatalf("community prior missing on fast: dur=%v samples=%d ok=%v", dur, samples, ok)
	}

	// Heartbeat telemetry (0.2/0.5)
	_, err = srv.Heartbeat(ctx, &omlsv1.HeartbeatRequest{
		NodeId: "node-fast", SessionId: mustSession(t, reg, "node-fast"),
		Telemetry: &omlsv1.TelemetrySnapshot{CpuLoad: 0.2, TemperatureC: 40, TemperatureKnown: true, ObservedUnixMs: time.Now().UnixMilli()},
	})
	if err != nil {
		t.Fatal(err)
	}

	health, err := srv.Health(ctx, &omlsv1.HealthRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if health.GetAvailableNodes() < 2 {
		t.Fatalf("health=%+v", health)
	}
	if health.GetVersion() != "1.3.0" {
		t.Fatalf("health version=%s", health.GetVersion())
	}

	// 0.4 envelope
	env, err := envelope.Preset("eco")
	if err != nil {
		t.Fatal(err)
	}
	envYAML, err := env.MarshalYAML()
	if err != nil {
		t.Fatal(err)
	}
	// 0.9 power budget from envelope
	intensity := power.IntensityFromEnvelopeYAML(envYAML)
	b := power.EnvelopeBudgetHint("stack-smoke", intensity, srv.Power().State().TotalBudgetW, 0)
	ack := srv.Power().Handle(power.Message{Type: power.MsgSetBudget, SchemaVersion: power.SchemaVersion, Budget: &b})
	if !ack.OK {
		t.Fatal(ack.Error)
	}

	conn, client, err := fabric.Dial(ctx, fabric.DialOptions{
		Addr: lis.Addr().String(), TLS: insecure.NewCredentials(), Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	go fakeWorker(ctx, t, client, "node-fast", 15)
	go fakeWorker(ctx, t, client, "node-slow", 90)

	// 0.3/0.6 run-demo with adaptive policy + envelope
	resp, err := client.RunDemo(ctx, &omlsv1.RunDemoRequest{
		Workers:        8,
		Iterations:     4,
		WorkIterations: 1000,
		TimeoutMs:      20000,
		EnvelopeYaml:   envYAML,
		Policy:         learn.PolicyAdaptive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Rounds) != 4 {
		t.Fatalf("rounds=%d", len(resp.Rounds))
	}
	if !strings.Contains(resp.Rounds[0].PolicyNote, "policy=adaptive") {
		t.Fatalf("policy note=%s", resp.Rounds[0].PolicyNote)
	}
	if !strings.Contains(resp.Rounds[0].PolicyNote, "power:") {
		t.Fatalf("expected power note: %s", resp.Rounds[0].PolicyNote)
	}
	if !strings.Contains(resp.Summary, "power_draw=") {
		t.Fatalf("summary=%s", resp.Summary)
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
		t.Fatalf("adaptive should prefer fast: fast=%d slow=%d note=%s", fastW, slowW, last.PolicyNote)
	}

	// 0.5 behavior profiles persisted
	profiles, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) < 1 {
		t.Fatal("expected behavior profiles on disk")
	}

	// Graph dump includes both nodes
	g, err := srv.GetGraph(ctx, &omlsv1.GetGraphRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(g.GraphYaml), "node-fast") || !strings.Contains(string(g.GraphYaml), "sandbox") {
		t.Fatalf("graph missing expected content (%d bytes)", len(g.GraphYaml))
	}
}

func mustSession(t *testing.T, reg *graph.Registry, nodeID string) string {
	t.Helper()
	for _, n := range reg.Snapshot().Nodes {
		if n.ID == nodeID {
			return n.SessionID
		}
	}
	t.Fatalf("session for %s", nodeID)
	return ""
}
