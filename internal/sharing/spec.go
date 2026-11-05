// Package sharing owns task lifetimes and persistent, content-addressed results.
// Each machine has its own root; network transport is a separate concern.
package sharing

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

type Input struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}
type Spec struct {
	Schema        int             `json:"schema"`
	Kind          string          `json:"kind"`
	Operation     string          `json:"operation"`
	Model         string          `json:"model,omitempty"`
	Renderer      string          `json:"renderer"`
	Version       string          `json:"renderer_version"`
	Compatibility string          `json:"compatibility"`
	Inputs        []Input         `json:"inputs"`
	Parameters    json.RawMessage `json:"parameters"`
}

func validHash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == hex.EncodeToString(b)
}

// Canonical returns a stable JSON key document. Input order is significant;
// object member order is not. Numeric representations remain conservative:
// 1 and 1.0 may miss each other's cache but cannot reuse incompatible work.
func (s Spec) Canonical() ([]byte, error) {
	if s.Schema != 1 {
		return nil, fmt.Errorf("sharing spec schema must be 1")
	}
	if s.Kind != "graphics" && s.Kind != "compute" {
		return nil, fmt.Errorf("kind must be graphics or compute")
	}
	if s.Operation == "" || s.Renderer == "" || s.Version == "" || s.Compatibility == "" {
		return nil, fmt.Errorf("operation, renderer, renderer_version and compatibility are required")
	}
	names := map[string]bool{}
	for _, in := range s.Inputs {
		if in.Name == "" || names[in.Name] || !validHash(in.SHA256) {
			return nil, fmt.Errorf("inputs require distinct names and lowercase SHA-256 digests")
		}
		names[in.Name] = true
	}
	if len(s.Parameters) == 0 {
		s.Parameters = json.RawMessage(`{}`)
	}
	var params map[string]any
	d := json.NewDecoder(bytes.NewReader(s.Parameters))
	d.UseNumber()
	if err := d.Decode(&params); err != nil {
		return nil, fmt.Errorf("parameters must be a JSON object: %w", err)
	}
	if params == nil {
		return nil, fmt.Errorf("parameters must be a JSON object")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("trailing parameters data")
	}
	s.Parameters, _ = json.Marshal(params)
	if s.Inputs == nil {
		s.Inputs = []Input{}
	}
	return json.Marshal(s)
}
func (s Spec) Key() (string, error) {
	b, e := s.Canonical()
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func DecodeSpec(r io.Reader) (Spec, error) {
	var s Spec
	d := json.NewDecoder(io.LimitReader(r, 1<<20))
	d.DisallowUnknownFields()
	if e := d.Decode(&s); e != nil {
		return s, e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return s, fmt.Errorf("trailing spec data")
	}
	_, e := s.Canonical()
	return s, e
}
