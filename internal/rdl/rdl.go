// Package rdl defines Resource Description Language v0 (Machine Profile).
package rdl

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const Version = "0.1"

// Document is an RDL v0 Machine Profile.
type Document struct {
	RDLVersion string      `json:"rdl_version" yaml:"rdl_version"`
	Node       Node        `json:"node" yaml:"node"`
	Resources  []Resource  `json:"resources" yaml:"resources"`
	Transports []Transport `json:"transports" yaml:"transports"`
	Limits     []any       `json:"limits" yaml:"limits"`
	Behavior   []any       `json:"behavior" yaml:"behavior"`
}

// Node describes the local machine.
type Node struct {
	ID       string   `json:"id" yaml:"id"`
	Hostname string   `json:"hostname" yaml:"hostname"`
	OS       OSInfo   `json:"os" yaml:"os"`
	Virt     string   `json:"virt,omitempty" yaml:"virt,omitempty"`
	Warnings []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// OSInfo is OS identity for the node.
type OSInfo struct {
	Family string `json:"family" yaml:"family"`
	Pretty string `json:"pretty,omitempty" yaml:"pretty,omitempty"`
	Kernel string `json:"kernel,omitempty" yaml:"kernel,omitempty"`
	Arch   string `json:"arch,omitempty" yaml:"arch,omitempty"`
}

// Resource is a discovered device or logical capability unit.
type Resource struct {
	ID           string         `json:"id" yaml:"id"`
	Kind         string         `json:"kind" yaml:"kind"`   // device | logical
	Class        string         `json:"class" yaml:"class"` // compute, memory, ...
	Capabilities []string       `json:"capabilities" yaml:"capabilities"`
	Attrs        map[string]any `json:"attrs,omitempty" yaml:"attrs,omitempty"`
}

// Transport is a network (or similar) path off the node.
type Transport struct {
	ID    string         `json:"id" yaml:"id"`
	Type  string         `json:"type" yaml:"type"` // ethernet | wifi | loopback | other
	Attrs map[string]any `json:"attrs,omitempty" yaml:"attrs,omitempty"`
}

// Empty returns a valid empty shell document (limits/behavior always present).
func Empty() Document {
	return Document{
		RDLVersion: Version,
		Resources:  []Resource{},
		Transports: []Transport{},
		Limits:     []any{},
		Behavior:   []any{},
	}
}

// Validate checks structural invariants for RDL v0.
func (d *Document) Validate() error {
	if d == nil {
		return fmt.Errorf("document is nil")
	}
	if d.RDLVersion != Version {
		return fmt.Errorf("rdl_version: want %q, got %q", Version, d.RDLVersion)
	}
	if strings.TrimSpace(d.Node.ID) == "" {
		return fmt.Errorf("node.id is required")
	}
	if strings.TrimSpace(d.Node.Hostname) == "" {
		return fmt.Errorf("node.hostname is required")
	}
	if d.Node.OS.Family != "linux" {
		return fmt.Errorf("node.os.family: want %q, got %q", "linux", d.Node.OS.Family)
	}
	if d.Resources == nil {
		return fmt.Errorf("resources must be present (may be empty)")
	}
	if d.Transports == nil {
		return fmt.Errorf("transports must be present (may be empty)")
	}
	if d.Limits == nil {
		return fmt.Errorf("limits must be present (may be empty)")
	}
	if d.Behavior == nil {
		return fmt.Errorf("behavior must be present (may be empty)")
	}

	seen := map[string]struct{}{}
	for i, r := range d.Resources {
		if err := validateResource(i, r); err != nil {
			return err
		}
		if _, ok := seen[r.ID]; ok {
			return fmt.Errorf("resources[%d].id %q is duplicated", i, r.ID)
		}
		seen[r.ID] = struct{}{}
	}
	tseen := map[string]struct{}{}
	for i, t := range d.Transports {
		if err := validateTransport(i, t); err != nil {
			return err
		}
		if _, ok := tseen[t.ID]; ok {
			return fmt.Errorf("transports[%d].id %q is duplicated", i, t.ID)
		}
		tseen[t.ID] = struct{}{}
	}
	return nil
}

func validateResource(i int, r Resource) error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("resources[%d].id is required", i)
	}
	switch r.Kind {
	case "device", "logical":
	default:
		return fmt.Errorf("resources[%d].kind: invalid %q", i, r.Kind)
	}
	switch r.Class {
	case "compute", "memory", "storage", "network", "graphics", "pci", "usb", "thermal", "power", "sandbox", "other":
	default:
		return fmt.Errorf("resources[%d].class: invalid %q", i, r.Class)
	}
	if len(r.Capabilities) == 0 {
		return fmt.Errorf("resources[%d].capabilities must be non-empty", i)
	}
	for j, c := range r.Capabilities {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("resources[%d].capabilities[%d] is empty", i, j)
		}
	}
	return nil
}

func validateTransport(i int, t Transport) error {
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf("transports[%d].id is required", i)
	}
	switch t.Type {
	case "ethernet", "wifi", "loopback", "other":
	default:
		return fmt.Errorf("transports[%d].type: invalid %q", i, t.Type)
	}
	return nil
}

// MarshalYAML encodes the document as YAML.
func (d *Document) MarshalYAML() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return yaml.Marshal(d)
}

// MarshalJSON encodes the document as JSON.
func (d *Document) MarshalJSON() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	type alias Document
	return json.MarshalIndent((*alias)(d), "", "  ")
}

// ParseYAML parses and validates an RDL document from YAML bytes.
func ParseYAML(data []byte) (*Document, error) {
	var d Document
	if err := yaml.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	normalize(&d)
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

// ParseJSON parses and validates an RDL document from JSON bytes.
func ParseJSON(data []byte) (*Document, error) {
	var d Document
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	normalize(&d)
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

func normalize(d *Document) {
	if d.Resources == nil {
		d.Resources = []Resource{}
	}
	if d.Transports == nil {
		d.Transports = []Transport{}
	}
	if d.Limits == nil {
		d.Limits = []any{}
	}
	if d.Behavior == nil {
		d.Behavior = []any{}
	}
}

// WriteFile writes YAML (default) or JSON based on path suffix / format.
func WriteFile(path, format string, d *Document) error {
	if err := d.Validate(); err != nil {
		return err
	}
	var (
		data []byte
		err  error
	)
	switch strings.ToLower(format) {
	case "json":
		data, err = d.MarshalJSON()
		data = append(data, '\n')
	case "yaml", "yml", "":
		data, err = d.MarshalYAML()
	default:
		return fmt.Errorf("unsupported format %q (use yaml or json)", format)
	}
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// FormatFromPath guesses yaml vs json from the output path.
func FormatFromPath(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".json"):
		return "json"
	default:
		return "yaml"
	}
}
