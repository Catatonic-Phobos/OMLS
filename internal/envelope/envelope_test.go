package envelope_test

import (
	"context"
	"testing"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/envelope"
	"github.com/Catatonic-Phobos/OMLS/internal/work"
)

func TestParseAndValidate(t *testing.T) {
	raw := []byte(`
name: demo
performance: high
attack: { level: 0.8, duration: 100ms }
peak: { level: 0.9, duration: 200ms }
sustain: { level: 0.5, duration: 0s }
release: { mode: gradual, duration: 300ms }
limits: { thermal_max_c: 80 }
`)
	env, err := envelope.ParseYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if env.Attack.Duration.Std() != 100*time.Millisecond {
		t.Fatalf("attack dur %s", env.Attack.Duration.Std())
	}
	if env.Limits.ThermalMaxC != 80 {
		t.Fatalf("thermal %v", env.Limits.ThermalMaxC)
	}
}

func TestPresetPerformanceOnly(t *testing.T) {
	env, err := envelope.ParseYAML([]byte("performance: eco\n"))
	if err != nil {
		t.Fatal(err)
	}
	if env.Sustain.Level != 0.35 {
		t.Fatalf("%+v", env)
	}
}

func TestDutyCycleSlowsBurn(t *testing.T) {
	ctx := context.Background()
	full, err := work.Burn(ctx, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	period := 20 * time.Millisecond
	dc := envelope.NewDutyCycle(&period)
	_ = dc.SetLevel(ctx, 0.25)
	throttled, err := work.Burn(ctx, 2_000_000, dc)
	if err != nil {
		t.Fatal(err)
	}
	if throttled <= full {
		t.Fatalf("expected throttle to slow burn: full=%s throttled=%s", full, throttled)
	}
}

func TestControllerPhases(t *testing.T) {
	period := 10 * time.Millisecond
	dc := envelope.NewDutyCycle(&period)
	levels := []float64{}
	ctrl := &envelope.Controller{
		Env: envelope.Envelope{
			Name: "t", Performance: envelope.PerformanceBalanced,
			Attack:  envelope.Phase{Level: 0.8, Duration: envelope.Duration(20 * time.Millisecond)},
			Peak:    envelope.Phase{Level: 0.9, Duration: envelope.Duration(20 * time.Millisecond)},
			Sustain: envelope.Phase{Level: 0.4, Duration: envelope.Duration(30 * time.Millisecond)},
			Release: envelope.ReleasePhase{Mode: envelope.ReleaseGradual, Duration: envelope.Duration(20 * time.Millisecond)},
			Limits:  envelope.Limits{ThermalMaxC: 85},
		},
		Backends: []envelope.Backend{dc},
		OnLevel:  func(_ string, level float64) { levels = append(levels, level) },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rep, err := ctrl.Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) < 3 {
		t.Fatalf("levels=%v", levels)
	}
	if rep.FinalLevel != 0 {
		t.Fatalf("final=%v", rep.FinalLevel)
	}
	if len(rep.Backends) != 1 || rep.Backends[0] != "duty-cycle" {
		t.Fatalf("%+v", rep)
	}
}

func TestThermalClamp(t *testing.T) {
	period := 10 * time.Millisecond
	dc := envelope.NewDutyCycle(&period)
	ctrl := &envelope.Controller{
		Env: envelope.Envelope{
			Name: "hot", Performance: envelope.PerformanceHigh,
			Attack:  envelope.Phase{Level: 0.9, Duration: 0},
			Peak:    envelope.Phase{Level: 0.9, Duration: 0},
			Sustain: envelope.Phase{Level: 0.9, Duration: envelope.Duration(10 * time.Millisecond)},
			Release: envelope.ReleasePhase{Mode: envelope.ReleaseImmediate, Duration: 0},
			Limits:  envelope.Limits{ThermalMaxC: 60},
		},
		Backends: []envelope.Backend{dc},
		TempFn:   func() (float64, bool) { return 90, true },
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	rep, err := ctrl.Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range rep.Warnings {
		if w != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected thermal warning: %+v", rep)
	}
}
