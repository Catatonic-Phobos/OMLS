package fabric_test

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/behavior"
	"github.com/Catatonic-Phobos/OMLS/internal/fabric"
	"github.com/Catatonic-Phobos/OMLS/internal/fabric/tlsconfig"
	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	omlsv1 "github.com/Catatonic-Phobos/OMLS/proto/omls/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func TestFabricInsecureTwoAgents(t *testing.T) {
	reg := graph.New(200 * time.Millisecond)
	store, err := behavior.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	omlsv1.RegisterFabricServer(gs, fabric.NewServerWithStore(reg, 50*time.Millisecond, store))
	go gs.Serve(lis) //nolint:errcheck
	defer gs.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for _, name := range []string{"a", "b"} {
		conn, client, err := fabric.Dial(ctx, fabric.DialOptions{
			Addr:    lis.Addr().String(),
			TLS:     insecure.NewCredentials(),
			Timeout: 3 * time.Second,
		})
		if err != nil {
			t.Fatalf("%s dial: %v", name, err)
		}
		if err := fabric.Run(ctx, fabric.AgentConfig{Client: client, Conn: conn, Once: true}); err != nil {
			t.Fatalf("%s run: %v", name, err)
		}
	}

	snap := reg.Snapshot()
	if len(snap.Nodes) != 1 {
		t.Fatalf("nodes=%d", len(snap.Nodes))
	}
	n := snap.Nodes[0]
	if n.Status != graph.StatusAvailable || n.Profile == nil {
		t.Fatalf("%+v", n)
	}
	hasCPU := false
	for _, r := range n.Profile.Resources {
		if r.Class == "compute" {
			hasCPU = true
		}
	}
	if !hasCPU {
		t.Fatal("missing compute resource in graph")
	}

	conn := mustDial(t, lis.Addr().String(), nil)
	health, err := omlsv1.NewFabricClient(conn).Health(ctx, &omlsv1.HealthRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if health.GetAvailableNodes() < 1 {
		t.Fatalf("%+v", health)
	}

	time.Sleep(250 * time.Millisecond)
	reg.Sweep()
	snap = reg.Snapshot()
	if snap.Nodes[0].Status != graph.StatusUnavailable {
		t.Fatalf("want unavailable, got %s", snap.Nodes[0].Status)
	}
}

func TestFabricMTLS(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(repoRoot(t), "scripts", "gen-dev-certs.sh")
	cmd := exec.Command("bash", script, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gen certs: %v\n%s", err, out)
	}

	serverTLS, err := tlsconfig.LoadServer(
		filepath.Join(dir, "ca.crt"),
		filepath.Join(dir, "master.crt"),
		filepath.Join(dir, "master.key"),
	)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS, err := tlsconfig.LoadClient(
		filepath.Join(dir, "ca.crt"),
		filepath.Join(dir, "agent.crt"),
		filepath.Join(dir, "agent.key"),
		"localhost",
	)
	if err != nil {
		t.Fatal(err)
	}

	reg := graph.New(time.Second)
	store, err := behavior.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	omlsv1.RegisterFabricServer(gs, fabric.NewServerWithStore(reg, time.Second, store))
	go gs.Serve(lis) //nolint:errcheck
	defer gs.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, client, err := fabric.Dial(ctx, fabric.DialOptions{
		Addr:    lis.Addr().String(),
		TLS:     credentials.NewTLS(clientTLS),
		Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := fabric.Run(ctx, fabric.AgentConfig{Client: client, Once: true}); err != nil {
		t.Fatal(err)
	}
	raw, err := client.GetGraph(ctx, &omlsv1.GetGraphRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw.GetGraphYaml()) == 0 {
		t.Fatal("empty graph")
	}
}

func mustDial(t *testing.T, addr string, tlsCred credentials.TransportCredentials) *grpc.ClientConn {
	t.Helper()
	if tlsCred == nil {
		tlsCred = insecure.NewCredentials()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := fabric.Dial(ctx, fabric.DialOptions{Addr: addr, TLS: tlsCred, Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// internal/fabric → repo root
	return filepath.Clean(filepath.Join(wd, "../.."))
}
