// Package community loads shared Machine+Behavior profile snippets (OMLS 0.7).
package community

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const SchemaVersion = "0.7"

// Confidence levels (provenance).
const (
	ConfidenceUnknown         = "unknown"
	ConfidenceExperimental    = "experimental"
	ConfidenceCommunityTested = "community_tested"
	ConfidenceVerified        = "verified"
	ConfidenceStable          = "stable"
)

// Provenance describes where a community profile came from.
type Provenance struct {
	Source          string  `json:"source,omitempty" yaml:"source,omitempty"` // e.g. community, local, vendor
	Author          string  `json:"author,omitempty" yaml:"author,omitempty"`
	URL             string  `json:"url,omitempty" yaml:"url,omitempty"`
	Confidence      string  `json:"confidence,omitempty" yaml:"confidence,omitempty"`
	ConfidenceScore float64 `json:"confidence_score,omitempty" yaml:"confidence_score,omitempty"` // 0..1
	Notes           string  `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// MatchRule is a best-effort heuristic for attaching a profile to a live node.
type MatchRule struct {
	Arch          []string `json:"arch,omitempty" yaml:"arch,omitempty"`
	Virt          []string `json:"virt,omitempty" yaml:"virt,omitempty"`
	ModelContains []string `json:"model_contains,omitempty" yaml:"model_contains,omitempty"`
	OSFamily      []string `json:"os_family,omitempty" yaml:"os_family,omitempty"`
}

// Priors are soft learning seeds applied when a node matches and has no local observations yet.
type Priors struct {
	DurationEWMAMs float64 `json:"duration_ewma_ms,omitempty" yaml:"duration_ewma_ms,omitempty"`
	TempDeltaEWMA  float64 `json:"temp_delta_ewma,omitempty" yaml:"temp_delta_ewma,omitempty"`
	Samples        int     `json:"samples,omitempty" yaml:"samples,omitempty"` // synthetic prior weight
	Notes          string  `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// Profile is a community hardware + behavior snippet.
type Profile struct {
	CommunityVersion string         `json:"community_version" yaml:"community_version"`
	ID               string         `json:"id" yaml:"id"`
	Title            string         `json:"title,omitempty" yaml:"title,omitempty"`
	Provenance       Provenance     `json:"provenance,omitempty" yaml:"provenance,omitempty"`
	Match            MatchRule      `json:"match,omitempty" yaml:"match,omitempty"`
	Priors           Priors         `json:"priors,omitempty" yaml:"priors,omitempty"`
	MachineHints     map[string]any `json:"machine_hints,omitempty" yaml:"machine_hints,omitempty"`
	// SourcePath is set when loaded from disk (not serialized by default).
	SourcePath string `json:"-" yaml:"-"`
}

// NodeFacts are matcher inputs from a live RDL / graph node.
type NodeFacts struct {
	Arch     string
	Virt     string
	CPUModel string
	OSFamily string
	Hostname string
}

// Validate checks schema invariants.
func (p *Profile) Validate() error {
	if p == nil {
		return fmt.Errorf("profile is nil")
	}
	if p.CommunityVersion != SchemaVersion {
		return fmt.Errorf("community_version: want %q, got %q", SchemaVersion, p.CommunityVersion)
	}
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if p.Provenance.Confidence != "" {
		switch strings.ToLower(p.Provenance.Confidence) {
		case ConfidenceUnknown, ConfidenceExperimental, ConfidenceCommunityTested, ConfidenceVerified, ConfidenceStable:
		default:
			return fmt.Errorf("provenance.confidence: unknown level %q", p.Provenance.Confidence)
		}
	}
	if p.Provenance.ConfidenceScore < 0 || p.Provenance.ConfidenceScore > 1 {
		return fmt.Errorf("provenance.confidence_score must be 0..1")
	}
	return nil
}

// ParseYAML unmarshals and validates a community profile.
func ParseYAML(raw []byte) (Profile, error) {
	var p Profile
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return Profile{}, err
	}
	if err := p.Validate(); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// LoadFile reads one community profile YAML.
func LoadFile(path string) (Profile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	p, err := ParseYAML(b)
	if err != nil {
		return Profile{}, fmt.Errorf("%s: %w", path, err)
	}
	p.SourcePath = path
	return p, nil
}

// Catalog is a directory of community profiles.
type Catalog struct {
	Dir      string
	Profiles []Profile
}

// DefaultDirs are searched relative to cwd when no dir is given.
var DefaultDirs = []string{"community", "profiles", "examples/community"}

// Open loads all *.yaml from dir. Missing dir → empty catalog (no error).
func Open(dir string) (*Catalog, error) {
	if dir == "" {
		dir = "community"
	}
	c := &Catalog{Dir: dir, Profiles: []Profile{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		p, err := LoadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		c.Profiles = append(c.Profiles, p)
	}
	sort.Slice(c.Profiles, func(i, j int) bool { return c.Profiles[i].ID < c.Profiles[j].ID })
	return c, nil
}

// ResolveDir picks the first existing default directory, or fallback.
func ResolveDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	for _, d := range DefaultDirs {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
	}
	return "community"
}

// Get returns a profile by id.
func (c *Catalog) Get(id string) (Profile, bool) {
	for _, p := range c.Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

// ImportFile copies/validates a profile into the catalog directory.
func ImportFile(dir, srcPath string) (Profile, error) {
	if dir == "" {
		dir = "community"
	}
	p, err := LoadFile(srcPath)
	if err != nil {
		return Profile{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Profile{}, err
	}
	raw, err := yaml.Marshal(p)
	if err != nil {
		return Profile{}, err
	}
	// Drop SourcePath by re-marshaling a clean copy.
	clean := p
	clean.SourcePath = ""
	raw, err = yaml.Marshal(clean)
	if err != nil {
		return Profile{}, err
	}
	dst := filepath.Join(dir, sanitizeFile(p.ID)+".yaml")
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		return Profile{}, err
	}
	p.SourcePath = dst
	return p, nil
}

func sanitizeFile(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "profile"
	}
	return b.String()
}

// MatchScore returns how well a profile matches node facts (0 = no match).
// Higher is better. Requires at least one non-empty match criterion to succeed.
func (p Profile) MatchScore(facts NodeFacts) int {
	rule := p.Match
	hasCriterion := len(rule.Arch)+len(rule.Virt)+len(rule.ModelContains)+len(rule.OSFamily) > 0
	if !hasCriterion {
		return 0
	}
	score := 0
	if len(rule.Arch) > 0 {
		if !containsFold(rule.Arch, normalizeArch(facts.Arch)) && !containsFold(rule.Arch, facts.Arch) {
			return 0
		}
		score += 3
	}
	if len(rule.Virt) > 0 {
		if !containsFold(rule.Virt, facts.Virt) {
			return 0
		}
		score += 3
	}
	if len(rule.OSFamily) > 0 {
		if !containsFold(rule.OSFamily, facts.OSFamily) {
			return 0
		}
		score += 1
	}
	if len(rule.ModelContains) > 0 {
		model := strings.ToLower(facts.CPUModel)
		ok := false
		for _, sub := range rule.ModelContains {
			if sub != "" && strings.Contains(model, strings.ToLower(sub)) {
				ok = true
				score += 2
				break
			}
		}
		if !ok {
			return 0
		}
	}
	// Slight boost for higher confidence.
	switch strings.ToLower(p.Provenance.Confidence) {
	case ConfidenceStable:
		score += 2
	case ConfidenceVerified:
		score += 1
	case ConfidenceCommunityTested:
		score += 1
	}
	return score
}

// BestMatch returns the highest-scoring profile for facts, or false if none match.
func (c *Catalog) BestMatch(facts NodeFacts) (Profile, int, bool) {
	best := Profile{}
	bestScore := 0
	found := false
	for _, p := range c.Profiles {
		sc := p.MatchScore(facts)
		if sc <= 0 {
			continue
		}
		if !found || sc > bestScore || (sc == bestScore && p.ID < best.ID) {
			best = p
			bestScore = sc
			found = true
		}
	}
	return best, bestScore, found
}

func containsFold(list []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return false
	}
	for _, item := range list {
		if strings.ToLower(strings.TrimSpace(item)) == want {
			return true
		}
	}
	return false
}

func normalizeArch(arch string) string {
	a := strings.ToLower(strings.TrimSpace(arch))
	switch a {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return a
	}
}

// ScoreToConfidence maps numeric score to a display label (helper for CLI).
func ScoreLabel(score float64) string {
	switch {
	case score >= 0.85:
		return ConfidenceStable
	case score >= 0.7:
		return ConfidenceVerified
	case score >= 0.5:
		return ConfidenceCommunityTested
	case score >= 0.25:
		return ConfidenceExperimental
	default:
		return ConfidenceUnknown
	}
}
