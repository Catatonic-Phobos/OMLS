// Package envelope defines OMLS 0.4 Resource Envelopes and applies them
// best-effort onto stock Linux controls (userspace duty-cycle, cgroup v2
// cpu.max, CPUFreq when present).
package envelope

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Performance is a coarse intent knob.
type Performance string

const (
	PerformanceHigh     Performance = "high"
	PerformanceBalanced Performance = "balanced"
	PerformanceEco      Performance = "eco"
)

// ReleaseMode controls how capacity ramps down.
type ReleaseMode string

const (
	ReleaseGradual   ReleaseMode = "gradual"
	ReleaseImmediate ReleaseMode = "immediate"
)

// Phase is one ADSR segment.
type Phase struct {
	Level    float64  `json:"level" yaml:"level"` // 0..1 of capacity
	Duration Duration `json:"duration" yaml:"duration"`
}

// Limits are hard-ish operational ceilings.
type Limits struct {
	ThermalMaxC float64 `json:"thermal_max_c,omitempty" yaml:"thermal_max_c,omitempty"`
}

// ReleasePhase is the release segment (mode + duration; level always ramps to 0).
type ReleasePhase struct {
	Mode     ReleaseMode `json:"mode" yaml:"mode"`
	Duration Duration    `json:"duration" yaml:"duration"`
}

// Envelope is a Resource Envelope (synth-style ADSR over compute capacity).
type Envelope struct {
	Name        string       `json:"name,omitempty" yaml:"name,omitempty"`
	Performance Performance  `json:"performance,omitempty" yaml:"performance,omitempty"`
	Attack      Phase        `json:"attack" yaml:"attack"`
	Peak        Phase        `json:"peak" yaml:"peak"`
	Sustain     Phase        `json:"sustain" yaml:"sustain"`
	Release     ReleasePhase `json:"release" yaml:"release"`
	Limits      Limits       `json:"limits,omitempty" yaml:"limits,omitempty"`
}

// Default returns a balanced envelope suitable for demos.
func Default() Envelope {
	var e Envelope
	e.Name = "balanced"
	e.Performance = PerformanceBalanced
	e.Attack = Phase{Level: 0.70, Duration: Duration(200_000_000)}
	e.Peak = Phase{Level: 0.85, Duration: Duration(300_000_000)}
	e.Sustain = Phase{Level: 0.55, Duration: 0}
	e.Release = ReleasePhase{Mode: ReleaseGradual, Duration: Duration(300_000_000)}
	e.Limits.ThermalMaxC = 85
	return e
}

// HighBurst is a more aggressive profile.
func HighBurst() Envelope {
	e := Default()
	e.Name = "high-burst"
	e.Performance = PerformanceHigh
	e.Attack = Phase{Level: 0.80, Duration: Duration(100_000_000)}
	e.Peak = Phase{Level: 0.95, Duration: Duration(500_000_000)}
	e.Sustain = Phase{Level: 0.70, Duration: 0}
	e.Release.Duration = Duration(500_000_000)
	return e
}

// Eco is a restrained profile.
func Eco() Envelope {
	e := Default()
	e.Name = "eco"
	e.Performance = PerformanceEco
	e.Attack = Phase{Level: 0.40, Duration: Duration(300_000_000)}
	e.Peak = Phase{Level: 0.50, Duration: Duration(200_000_000)}
	e.Sustain = Phase{Level: 0.35, Duration: 0}
	e.Release.Duration = Duration(400_000_000)
	e.Limits.ThermalMaxC = 75
	return e
}

// Validate checks envelope invariants.
func (e Envelope) Validate() error {
	if e.Performance != "" {
		switch e.Performance {
		case PerformanceHigh, PerformanceBalanced, PerformanceEco:
		default:
			return fmt.Errorf("performance: invalid %q", e.Performance)
		}
	}
	for _, p := range []struct {
		name string
		ph   Phase
	}{
		{"attack", e.Attack},
		{"peak", e.Peak},
		{"sustain", e.Sustain},
	} {
		if p.ph.Level < 0 || p.ph.Level > 1 {
			return fmt.Errorf("%s.level: want 0..1, got %v", p.name, p.ph.Level)
		}
		if p.ph.Duration < 0 {
			return fmt.Errorf("%s.duration: negative", p.name)
		}
	}
	switch e.Release.Mode {
	case "", ReleaseGradual, ReleaseImmediate:
	default:
		return fmt.Errorf("release.mode: invalid %q", e.Release.Mode)
	}
	if e.Release.Duration < 0 {
		return fmt.Errorf("release.duration: negative")
	}
	if e.Limits.ThermalMaxC < 0 {
		return fmt.Errorf("limits.thermal_max_c: negative")
	}
	return nil
}

// Normalize fills defaults for omitted fields.
func (e Envelope) Normalize() Envelope {
	out := e
	if out.Performance == "" {
		out.Performance = PerformanceBalanced
	}
	if out.Release.Mode == "" {
		out.Release.Mode = ReleaseGradual
	}
	if out.Name == "" {
		out.Name = string(out.Performance)
	}
	if out.Limits.ThermalMaxC == 0 {
		out.Limits.ThermalMaxC = 85
	}
	if out.Attack.Level == 0 && out.Peak.Level == 0 && out.Sustain.Level == 0 {
		switch out.Performance {
		case PerformanceHigh:
			base := HighBurst()
			base.Name = out.Name
			return base
		case PerformanceEco:
			base := Eco()
			base.Name = out.Name
			return base
		default:
			base := Default()
			base.Name = out.Name
			return base
		}
	}
	return out
}

// ParseYAML loads and validates an envelope.
func ParseYAML(data []byte) (Envelope, error) {
	var e Envelope
	if err := yaml.Unmarshal(data, &e); err != nil {
		return Envelope{}, fmt.Errorf("yaml: %w", err)
	}
	e = e.Normalize()
	if err := e.Validate(); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// LoadFile reads an envelope YAML file.
func LoadFile(path string) (Envelope, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Envelope{}, err
	}
	return ParseYAML(b)
}

// MarshalYAML encodes the envelope.
func (e Envelope) MarshalYAML() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return yaml.Marshal(e)
}

// Preset returns a named built-in envelope.
func Preset(name string) (Envelope, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "default", "balanced":
		return Default(), nil
	case "high", "high-burst", "burst":
		return HighBurst(), nil
	case "eco", "low":
		return Eco(), nil
	default:
		return Envelope{}, fmt.Errorf("unknown envelope preset %q (balanced|high|eco)", name)
	}
}
