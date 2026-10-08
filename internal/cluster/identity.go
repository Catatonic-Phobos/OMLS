package cluster

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Identity is the persistent cluster identity of one OMLS installation.
// A reboot must keep the same node_id.
type Identity struct {
	NodeID string `yaml:"node_id"`
}

// LoadOrCreate reads identity.yaml from dir. The file wins over discoveredID
// so a later machine-id change cannot split a known cluster member.
// When no file exists, discoveredID (typically /etc/machine-id) is stored.
func LoadOrCreate(dir, discoveredID string) (Identity, error) {
	if dir == "" {
		return Identity{}, fmt.Errorf("data dir is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Identity{}, err
	}
	path := filepath.Join(dir, "identity.yaml")
	raw, err := os.ReadFile(path)
	if err == nil {
		var id Identity
		if err := yaml.Unmarshal(raw, &id); err != nil {
			return Identity{}, fmt.Errorf("identity %s: %w", path, err)
		}
		id.NodeID = strings.TrimSpace(id.NodeID)
		if id.NodeID == "" {
			return Identity{}, fmt.Errorf("identity %s: node_id is empty", path)
		}
		return id, nil
	}
	if !os.IsNotExist(err) {
		return Identity{}, err
	}
	id := Identity{NodeID: strings.TrimSpace(discoveredID)}
	if id.NodeID == "" {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return Identity{}, err
		}
		id.NodeID = hex.EncodeToString(b[:])
	}
	if err := SaveIdentity(dir, id); err != nil {
		return Identity{}, err
	}
	return id, nil
}

// SaveIdentity writes identity.yaml.
func SaveIdentity(dir string, id Identity) error {
	if strings.TrimSpace(id.NodeID) == "" {
		return fmt.Errorf("node_id is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := yaml.Marshal(id)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "identity.yaml")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
