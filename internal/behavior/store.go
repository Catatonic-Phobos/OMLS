// Package behavior stores OMLS 0.5 Behavior Profiles and telemetry history.
package behavior

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const ProfileVersion = "0.5"

// TelemetrySample is one persisted host sample.
type TelemetrySample struct {
	ObservedAt        time.Time `json:"observed_at" yaml:"observed_at"`
	CPULoad           float64   `json:"cpu_load" yaml:"cpu_load"`
	MemAvailableBytes int64     `json:"mem_available_bytes" yaml:"mem_available_bytes"`
	MemTotalBytes     int64     `json:"mem_total_bytes" yaml:"mem_total_bytes"`
	TemperatureC      float64   `json:"temperature_c,omitempty" yaml:"temperature_c,omitempty"`
	TemperatureKnown  bool      `json:"temperature_known" yaml:"temperature_known"`
}

// ResourceObserved is learned stats for one resource (usually compute).
type ResourceObserved struct {
	DurationEWMAMs float64   `json:"duration_ewma_ms" yaml:"duration_ewma_ms"`
	TempDeltaEWMA  float64   `json:"temp_delta_ewma" yaml:"temp_delta_ewma"`
	TempEWMAC      float64   `json:"temp_ewma_c,omitempty" yaml:"temp_ewma_c,omitempty"`
	TempKnown      bool      `json:"temp_known" yaml:"temp_known"`
	Samples        int       `json:"samples" yaml:"samples"`
	LastDurationMs float64   `json:"last_duration_ms,omitempty" yaml:"last_duration_ms,omitempty"`
	LastSeen       time.Time `json:"last_seen,omitempty" yaml:"last_seen,omitempty"`
}

// ResourceProfile is one resource entry inside a node Behavior Profile.
type ResourceProfile struct {
	ID         string           `json:"id" yaml:"id"`
	Class      string           `json:"class" yaml:"class"`
	Observed   ResourceObserved `json:"observed" yaml:"observed"`
	Confidence float64          `json:"confidence" yaml:"confidence"` // 0..1
}

// Profile is the persisted Behavior Profile for one node.
type Profile struct {
	ProfileVersion string            `json:"profile_version" yaml:"profile_version"`
	NodeID         string            `json:"node_id" yaml:"node_id"`
	Hostname       string            `json:"hostname,omitempty" yaml:"hostname,omitempty"`
	Virt           string            `json:"virt,omitempty" yaml:"virt,omitempty"`
	UpdatedAt      time.Time         `json:"updated_at" yaml:"updated_at"`
	Resources      []ResourceProfile `json:"resources" yaml:"resources"`
	// RecentTelemetry is a short ring kept inside the profile for quick show.
	RecentTelemetry []TelemetrySample `json:"recent_telemetry,omitempty" yaml:"recent_telemetry,omitempty"`
}

// Confidence maps sample count to 0..1 (1 - e^(-n/20)).
func Confidence(samples int) float64 {
	if samples <= 0 {
		return 0
	}
	c := 1 - math.Exp(-float64(samples)/20.0)
	if c > 1 {
		return 1
	}
	return c
}

// Store is a filesystem-backed Behavior Profile + telemetry store.
type Store struct {
	mu      sync.Mutex
	dir     string
	ringMax int
	alpha   float64
}

// Open creates/opens a store under dir (created if missing).
func Open(dir string) (*Store, error) {
	if dir == "" {
		dir = "omls-data"
	}
	if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "telemetry"), 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir, ringMax: 64, alpha: 0.3}, nil
}

// Dir returns the root data directory.
func (s *Store) Dir() string { return s.dir }

func (s *Store) profilePath(nodeID string) string {
	safe := sanitizeID(nodeID)
	return filepath.Join(s.dir, "profiles", safe+".yaml")
}

func (s *Store) telemetryPath(nodeID string) string {
	safe := sanitizeID(nodeID)
	return filepath.Join(s.dir, "telemetry", safe+".jsonl")
}

func sanitizeID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Load returns a profile or a fresh empty shell.
func (s *Store) Load(nodeID string) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadUnlocked(nodeID)
}

func (s *Store) loadUnlocked(nodeID string) (Profile, error) {
	path := s.profilePath(nodeID)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Profile{
				ProfileVersion: ProfileVersion,
				NodeID:         nodeID,
				Resources:      []ResourceProfile{},
			}, nil
		}
		return Profile{}, err
	}
	var p Profile
	if err := yaml.Unmarshal(b, &p); err != nil {
		return Profile{}, fmt.Errorf("profile %s: %w", path, err)
	}
	if p.ProfileVersion == "" {
		p.ProfileVersion = ProfileVersion
	}
	if p.NodeID == "" {
		p.NodeID = nodeID
	}
	if p.Resources == nil {
		p.Resources = []ResourceProfile{}
	}
	return p, nil
}

// Save writes a profile atomically.
func (s *Store) Save(p Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveUnlocked(p)
}

func (s *Store) saveUnlocked(p Profile) error {
	if p.NodeID == "" {
		return fmt.Errorf("node_id is required")
	}
	p.ProfileVersion = ProfileVersion
	p.UpdatedAt = time.Now().UTC()
	for i := range p.Resources {
		p.Resources[i].Confidence = Confidence(p.Resources[i].Observed.Samples)
	}
	raw, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	path := s.profilePath(p.NodeID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// List returns all profiles sorted by node id.
func (s *Store) List() ([]Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(s.dir, "profiles"))
	if err != nil {
		return nil, err
	}
	out := []Profile{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, "profiles", e.Name()))
		if err != nil {
			return nil, err
		}
		var p Profile
		if err := yaml.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out, nil
}

// RecordTelemetry appends a sample to jsonl history and the profile ring.
func (s *Store) RecordTelemetry(nodeID, hostname, virt string, sample TelemetrySample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sample.ObservedAt.IsZero() {
		sample.ObservedAt = time.Now().UTC()
	}
	line, err := json.Marshal(sample)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.telemetryPath(nodeID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Close()

	p, err := s.loadUnlocked(nodeID)
	if err != nil {
		return err
	}
	p.Hostname = hostname
	p.Virt = virt
	p.RecentTelemetry = append(p.RecentTelemetry, sample)
	if len(p.RecentTelemetry) > s.ringMax {
		p.RecentTelemetry = p.RecentTelemetry[len(p.RecentTelemetry)-s.ringMax:]
	}
	// Update compute resource temp EWMA when known.
	if sample.TemperatureKnown {
		rp := ensureResource(&p, "cpu0", "compute")
		if !rp.Observed.TempKnown || rp.Observed.Samples == 0 {
			rp.Observed.TempEWMAC = sample.TemperatureC
			rp.Observed.TempKnown = true
		} else {
			rp.Observed.TempEWMAC = s.alpha*sample.TemperatureC + (1-s.alpha)*rp.Observed.TempEWMAC
		}
	}
	return s.saveUnlocked(p)
}

// ObserveWork updates duration/temp-delta EWMAs after a work unit or round average.
func (s *Store) ObserveWork(nodeID, hostname, virt, resourceID string, durationMs, tempDelta float64, tempKnown bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.loadUnlocked(nodeID)
	if err != nil {
		return err
	}
	if hostname != "" {
		p.Hostname = hostname
	}
	if virt != "" {
		p.Virt = virt
	}
	if resourceID == "" {
		resourceID = "cpu0"
	}
	rp := ensureResource(&p, resourceID, "compute")
	obs := &rp.Observed
	if obs.Samples == 0 {
		obs.DurationEWMAMs = durationMs
		if tempKnown {
			obs.TempDeltaEWMA = tempDelta
		}
	} else {
		obs.DurationEWMAMs = s.alpha*durationMs + (1-s.alpha)*obs.DurationEWMAMs
		if tempKnown {
			obs.TempDeltaEWMA = s.alpha*tempDelta + (1-s.alpha)*obs.TempDeltaEWMA
		}
	}
	obs.LastDurationMs = durationMs
	obs.Samples++
	obs.LastSeen = time.Now().UTC()
	rp.Confidence = Confidence(obs.Samples)
	return s.saveUnlocked(p)
}

// DurationEWMA returns persisted duration EWMA for a node's compute resource.
func (s *Store) DurationEWMA(nodeID string) (float64, int, bool) {
	p, err := s.Load(nodeID)
	if err != nil {
		return 0, 0, false
	}
	for _, r := range p.Resources {
		if r.Class == "compute" || r.ID == "cpu0" {
			if r.Observed.Samples == 0 {
				return 0, 0, false
			}
			return r.Observed.DurationEWMAMs, r.Observed.Samples, true
		}
	}
	return 0, 0, false
}

// SeedPlaneStats returns map nodeID → (durationEWMA, tempDeltaEWMA, samples) for learning.
func (s *Store) SeedPlaneStats() (map[string]struct {
	Duration  float64
	TempDelta float64
	Samples   int
}, error) {
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	out := map[string]struct {
		Duration  float64
		TempDelta float64
		Samples   int
	}{}
	for _, p := range list {
		for _, r := range p.Resources {
			if r.Class != "compute" && r.ID != "cpu0" {
				continue
			}
			if r.Observed.Samples == 0 {
				continue
			}
			out[p.NodeID] = struct {
				Duration  float64
				TempDelta float64
				Samples   int
			}{Duration: r.Observed.DurationEWMAMs, TempDelta: r.Observed.TempDeltaEWMA, Samples: r.Observed.Samples}
			break
		}
	}
	return out, nil
}

// ApplyCommunityPrior seeds a Behavior Profile from a community snippet when the
// node has no local compute observations yet. Returns applied=false if skipped.
func (s *Store) ApplyCommunityPrior(nodeID, hostname, virt, communityID string, durationEWMA, tempDelta float64, samples int) (bool, error) {
	if samples <= 0 || nodeID == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.loadUnlocked(nodeID)
	if err != nil {
		return false, err
	}
	if hostname != "" {
		p.Hostname = hostname
	}
	if virt != "" {
		p.Virt = virt
	}
	rp := ensureResource(&p, "cpu0", "compute")
	if rp.Observed.Samples > 0 {
		return false, nil // local observations win
	}
	rp.Observed.DurationEWMAMs = durationEWMA
	rp.Observed.TempDeltaEWMA = tempDelta
	rp.Observed.Samples = samples
	rp.Observed.LastSeen = time.Now().UTC()
	rp.Confidence = Confidence(samples) * 0.5 // community prior is softer
	if communityID != "" {
		if p.RecentTelemetry == nil {
			p.RecentTelemetry = []TelemetrySample{}
		}
	}
	if err := s.saveUnlocked(p); err != nil {
		return false, err
	}
	return true, nil
}

func ensureResource(p *Profile, id, class string) *ResourceProfile {
	for i := range p.Resources {
		if p.Resources[i].ID == id {
			return &p.Resources[i]
		}
	}
	p.Resources = append(p.Resources, ResourceProfile{ID: id, Class: class})
	return &p.Resources[len(p.Resources)-1]
}

// ReadTelemetryTail returns the last n jsonl samples (n<=0 → 20).
func (s *Store) ReadTelemetryTail(nodeID string, n int) ([]TelemetrySample, error) {
	if n <= 0 {
		n = 20
	}
	path := s.telemetryPath(nodeID)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := make([]TelemetrySample, 0, len(lines))
	for _, line := range lines {
		var s TelemetrySample
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}
