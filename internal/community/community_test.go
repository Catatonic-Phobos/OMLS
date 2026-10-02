package community_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Catatonic-Phobos/OMLS/internal/community"
)

func TestParseAndMatch(t *testing.T) {
	raw := []byte(`
community_version: "0.7"
id: amd64-kvm-generic
title: Generic KVM amd64
provenance:
  source: community
  author: omls-examples
  confidence: community_tested
  confidence_score: 0.6
match:
  arch: [x86_64, amd64]
  virt: [kvm, qemu]
  os_family: [linux]
priors:
  duration_ewma_ms: 150
  temp_delta_ewma: 1.5
  samples: 8
`)
	p, err := community.ParseYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	sc := p.MatchScore(community.NodeFacts{Arch: "amd64", Virt: "kvm", OSFamily: "linux"})
	if sc <= 0 {
		t.Fatalf("expected match, score=%d", sc)
	}
	if p.MatchScore(community.NodeFacts{Arch: "aarch64", Virt: "kvm", OSFamily: "linux"}) != 0 {
		t.Fatal("arch mismatch should fail")
	}
	if p.MatchScore(community.NodeFacts{Arch: "x86_64", Virt: "wsl", OSFamily: "linux"}) != 0 {
		t.Fatal("virt mismatch should fail")
	}
}

func TestCatalogImportAndBestMatch(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src-wsl.yaml")
	content := []byte(`
community_version: "0.7"
id: wsl-amd64
title: WSL2 amd64
provenance:
  confidence: experimental
  confidence_score: 0.35
match:
  arch: [x86_64]
  virt: [wsl]
priors:
  duration_ewma_ms: 200
  samples: 5
`)
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}
	catDir := filepath.Join(dir, "community")
	p, err := community.ImportFile(catDir, src)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "wsl-amd64" {
		t.Fatalf("id=%s", p.ID)
	}
	c, err := community.Open(catDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Profiles) != 1 {
		t.Fatalf("len=%d", len(c.Profiles))
	}
	best, score, ok := c.BestMatch(community.NodeFacts{Arch: "x86_64", Virt: "wsl"})
	if !ok || best.ID != "wsl-amd64" || score <= 0 {
		t.Fatalf("best=%v score=%d ok=%v", best.ID, score, ok)
	}
}

func TestModelContains(t *testing.T) {
	p := community.Profile{
		CommunityVersion: community.SchemaVersion,
		ID:               "epyc",
		Match: community.MatchRule{
			Arch:          []string{"x86_64"},
			ModelContains: []string{"EPYC"},
		},
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.MatchScore(community.NodeFacts{Arch: "x86_64", CPUModel: "AMD EPYC 7763"}) <= 0 {
		t.Fatal("expected model match")
	}
	if p.MatchScore(community.NodeFacts{Arch: "x86_64", CPUModel: "Intel Xeon"}) != 0 {
		t.Fatal("expected model miss")
	}
}

func TestOpenMissingDir(t *testing.T) {
	c, err := community.Open(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Profiles) != 0 {
		t.Fatal("expected empty")
	}
}
