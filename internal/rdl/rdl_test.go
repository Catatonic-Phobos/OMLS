package rdl_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
)

func sampleDoc() rdl.Document {
	d := rdl.Empty()
	d.Node = rdl.Node{
		ID:       "abc123",
		Hostname: "lab-node",
		OS: rdl.OSInfo{
			Family: "linux",
			Pretty: "Debian GNU/Linux 12",
			Kernel: "6.1.0",
			Arch:   "x86_64",
		},
		Virt: "bare",
	}
	d.Resources = []rdl.Resource{
		{
			ID:           "cpu0",
			Kind:         "device",
			Class:        "compute",
			Capabilities: []string{"compute", "parallelizable"},
			Attrs:        map[string]any{"arch": "x86_64", "cores": 8},
		},
		{
			ID:           "mem0",
			Kind:         "device",
			Class:        "memory",
			Capabilities: []string{"memory"},
			Attrs:        map[string]any{"total_bytes": 8589934592},
		},
	}
	d.Transports = []rdl.Transport{
		{
			ID:   "eth0",
			Type: "ethernet",
			Attrs: map[string]any{
				"mtu": 1500,
			},
		},
	}
	return d
}

func TestValidateOK(t *testing.T) {
	d := sampleDoc()
	if err := d.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsBadVersion(t *testing.T) {
	d := sampleDoc()
	d.RDLVersion = "9.9"
	if err := d.Validate(); err == nil {
		t.Fatal("expected version error")
	}
}

func TestValidateRejectsEmptyCapabilities(t *testing.T) {
	d := sampleDoc()
	d.Resources[0].Capabilities = nil
	if err := d.Validate(); err == nil {
		t.Fatal("expected capabilities error")
	}
}

func TestRoundTripYAML(t *testing.T) {
	d := sampleDoc()
	raw, err := d.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	got, err := rdl.ParseYAML(raw)
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	if got.Node.ID != d.Node.ID || got.Node.Hostname != d.Node.Hostname {
		t.Fatalf("node mismatch: %+v", got.Node)
	}
	if len(got.Resources) != 2 {
		t.Fatalf("resources len=%d", len(got.Resources))
	}
	if len(got.Transports) != 1 || got.Transports[0].Type != "ethernet" {
		t.Fatalf("transports=%+v", got.Transports)
	}
	if got.Limits == nil || got.Behavior == nil {
		t.Fatal("limits/behavior must be non-nil after parse")
	}
}

func TestRoundTripJSON(t *testing.T) {
	d := sampleDoc()
	raw, err := d.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	got, err := rdl.ParseJSON(raw)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	if got.RDLVersion != rdl.Version {
		t.Fatalf("version=%q", got.RDLVersion)
	}
}

func TestWriteFileAndParse(t *testing.T) {
	d := sampleDoc()
	dir := t.TempDir()
	ypath := filepath.Join(dir, "machine-profile.yaml")
	if err := rdl.WriteFile(ypath, "yaml", &d); err != nil {
		t.Fatalf("WriteFile yaml: %v", err)
	}
	data, err := os.ReadFile(ypath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rdl.ParseYAML(data); err != nil {
		t.Fatalf("re-parse yaml: %v", err)
	}

	jpath := filepath.Join(dir, "machine-profile.json")
	if err := rdl.WriteFile(jpath, rdl.FormatFromPath(jpath), &d); err != nil {
		t.Fatalf("WriteFile json: %v", err)
	}
	jdata, err := os.ReadFile(jpath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rdl.ParseJSON(jdata); err != nil {
		t.Fatalf("re-parse json: %v", err)
	}
}

func TestParsePlanExampleShape(t *testing.T) {
	const sample = `
rdl_version: "0.1"
node:
  id: "stable-id"
  hostname: "server0"
  os:
    family: linux
    pretty: "Debian"
resources:
  - id: cpu0
    kind: device
    class: compute
    capabilities: [compute, parallelizable]
    attrs:
      arch: x86_64
      cores: 8
  - id: gpu0
    kind: device
    class: graphics
    capabilities: [graphics, compute]
    attrs:
      pci_class: "0300"
transports:
  - id: eth0
    type: ethernet
    attrs:
      mtu: 1500
limits: []
behavior: []
`
	doc, err := rdl.ParseYAML([]byte(sample))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	if len(doc.Resources) != 2 {
		t.Fatalf("want 2 resources, got %d", len(doc.Resources))
	}
}
