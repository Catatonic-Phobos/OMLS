package fabric_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/behavior"
	"github.com/Catatonic-Phobos/OMLS/internal/community"
	"github.com/Catatonic-Phobos/OMLS/internal/fabric"
	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
	omlsv1 "github.com/Catatonic-Phobos/OMLS/proto/omls/v1"
	"gopkg.in/yaml.v3"
)

func TestAdvertiseAppliesCommunityPrior(t *testing.T) {
	dir := t.TempDir()
	catDir := filepath.Join(dir, "community")
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(catDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prof := []byte(`
community_version: "0.7"
id: test-kvm
provenance:
  confidence: community_tested
  confidence_score: 0.6
match:
  arch: [x86_64]
  virt: [kvm]
priors:
  duration_ewma_ms: 123
  temp_delta_ewma: 2
  samples: 7
`)
	if err := os.WriteFile(filepath.Join(catDir, "test-kvm.yaml"), prof, 0o644); err != nil {
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
	reg := graph.New(15 * time.Second)
	srv := fabric.NewServerFull(reg, time.Second, store, cat)

	ctx := context.Background()
	regResp, err := srv.Register(ctx, &omlsv1.RegisterRequest{NodeId: "n1", Hostname: "h1", Virt: "kvm", AgentVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	doc := rdl.Empty()
	doc.Node.ID = "n1"
	doc.Node.Hostname = "h1"
	doc.Node.Virt = "kvm"
	doc.Node.OS.Family = "linux"
	doc.Node.OS.Arch = "x86_64"
	doc.Resources = []rdl.Resource{{
		ID: "cpu0", Kind: "device", Class: "compute",
		Capabilities: []string{"compute", "parallelizable"},
		Attrs:        map[string]any{"arch": "x86_64", "cores": 4},
	}}
	raw, _ := yaml.Marshal(doc)
	_, err = srv.AdvertiseProfile(ctx, &omlsv1.AdvertiseProfileRequest{
		NodeId: "n1", SessionId: regResp.SessionId, Format: "yaml", Profile: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	dur, samples, ok := store.DurationEWMA("n1")
	if !ok || samples != 7 || dur != 123 {
		t.Fatalf("prior not applied: dur=%v samples=%d ok=%v", dur, samples, ok)
	}
	// Second advertise should not overwrite after samples already present
	_, err = srv.AdvertiseProfile(ctx, &omlsv1.AdvertiseProfileRequest{
		NodeId: "n1", SessionId: regResp.SessionId, Format: "yaml", Profile: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	dur2, samples2, _ := store.DurationEWMA("n1")
	if dur2 != 123 || samples2 != 7 {
		t.Fatalf("prior should stick until ObserveWork: %v %d", dur2, samples2)
	}
}
