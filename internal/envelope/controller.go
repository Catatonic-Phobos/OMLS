package envelope

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Backend applies a capacity level (0..1) using one Linux or userspace mechanism.
type Backend interface {
	Name() string
	SetLevel(ctx context.Context, level float64) error
	Close() error
}

// Report summarizes what was applied.
type Report struct {
	Envelope   string   `json:"envelope" yaml:"envelope"`
	Backends   []string `json:"backends" yaml:"backends"`
	Warnings   []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	Phases     []string `json:"phases,omitempty" yaml:"phases,omitempty"`
	FinalLevel float64  `json:"final_level" yaml:"final_level"`
}

// Controller drives ADSR phases across one or more backends.
type Controller struct {
	Env      Envelope
	Backends []Backend
	// TempFn optionally returns current temperature Celsius.
	TempFn func() (temp float64, known bool)
	// OnLevel is invoked after each level change (tests / telemetry).
	OnLevel func(phase string, level float64)

	mu     sync.Mutex
	level  float64
	warns  []string
	phases []string
}

// Apply runs attack → peak → sustain (until ctx done or sustain duration) → release.
// For work integration, call Start and Stop around the workload instead.
func (c *Controller) Apply(ctx context.Context) (Report, error) {
	if err := c.startLocked(); err != nil {
		return Report{}, err
	}
	defer c.closeBackends()

	if err := c.runPhase(ctx, "attack", c.Env.Attack.Level, c.Env.Attack.Duration.Std()); err != nil {
		return c.report(), err
	}
	if err := c.runPhase(ctx, "peak", c.Env.Peak.Level, c.Env.Peak.Duration.Std()); err != nil {
		return c.report(), err
	}
	sustainDur := c.Env.Sustain.Duration.Std()
	if sustainDur > 0 {
		if err := c.runPhase(ctx, "sustain", c.Env.Sustain.Level, sustainDur); err != nil {
			return c.report(), err
		}
	} else {
		// Hold sustain until cancelled.
		if err := c.setLevel("sustain", c.Env.Sustain.Level); err != nil {
			return c.report(), err
		}
		<-ctx.Done()
	}
	_ = c.release(context.Background())
	return c.report(), nil
}

// Start applies attack+peak and leaves sustain active. Stop runs release.
func (c *Controller) Start(ctx context.Context) (Report, error) {
	if err := c.startLocked(); err != nil {
		return Report{}, err
	}
	if err := c.runPhase(ctx, "attack", c.Env.Attack.Level, c.Env.Attack.Duration.Std()); err != nil {
		c.closeBackends()
		return c.report(), err
	}
	if err := c.runPhase(ctx, "peak", c.Env.Peak.Level, c.Env.Peak.Duration.Std()); err != nil {
		c.closeBackends()
		return c.report(), err
	}
	if err := c.setLevel("sustain", c.Env.Sustain.Level); err != nil {
		c.closeBackends()
		return c.report(), err
	}
	return c.report(), nil
}

// Stop runs the release phase and closes backends.
func (c *Controller) Stop() (Report, error) {
	err := c.release(context.Background())
	c.closeBackends()
	return c.report(), err
}

// Level returns the current target level.
func (c *Controller) Level() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.level
}

func (c *Controller) startLocked() error {
	e := c.Env.Normalize()
	if err := e.Validate(); err != nil {
		return err
	}
	c.Env = e
	if len(c.Backends) == 0 {
		c.Backends = []Backend{NewDutyCycle(nil)}
		c.warns = append(c.warns, "no backends configured; using userspace duty-cycle")
	}
	c.phases = nil
	return nil
}

func (c *Controller) runPhase(ctx context.Context, name string, level float64, dur time.Duration) error {
	level = c.clampThermal(level)
	if err := c.setLevel(name, level); err != nil {
		return err
	}
	if dur <= 0 {
		return nil
	}
	t := time.NewTimer(dur)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Controller) release(ctx context.Context) error {
	mode := c.Env.Release.Mode
	if mode == "" {
		mode = ReleaseGradual
	}
	dur := c.Env.Release.Duration.Std()
	if mode == ReleaseImmediate || dur <= 0 {
		return c.setLevel("release", 0)
	}
	steps := 5
	start := c.Level()
	stepDur := dur / time.Duration(steps)
	for i := 1; i <= steps; i++ {
		lvl := start * (1 - float64(i)/float64(steps))
		if err := c.setLevel("release", lvl); err != nil {
			return err
		}
		t := time.NewTimer(stepDur)
		select {
		case <-ctx.Done():
			t.Stop()
			return c.setLevel("release", 0)
		case <-t.C:
		}
	}
	return c.setLevel("release", 0)
}

func (c *Controller) setLevel(phase string, level float64) error {
	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	level = c.clampThermal(level)
	c.mu.Lock()
	c.level = level
	c.phases = append(c.phases, fmt.Sprintf("%s=%.2f", phase, level))
	backends := append([]Backend{}, c.Backends...)
	c.mu.Unlock()
	if c.OnLevel != nil {
		c.OnLevel(phase, level)
	}
	var first error
	for _, b := range backends {
		if err := b.SetLevel(context.Background(), level); err != nil {
			c.mu.Lock()
			c.warns = append(c.warns, fmt.Sprintf("%s: %v", b.Name(), err))
			c.mu.Unlock()
			if first == nil {
				first = err
			}
		}
	}
	// Duty-cycle backend errors are fatal; optional backends warn only.
	if first != nil && len(backends) == 1 {
		return first
	}
	return nil
}

func (c *Controller) clampThermal(level float64) float64 {
	if c.TempFn == nil || c.Env.Limits.ThermalMaxC <= 0 {
		return level
	}
	temp, known := c.TempFn()
	if !known {
		return level
	}
	if temp < c.Env.Limits.ThermalMaxC {
		return level
	}
	c.mu.Lock()
	c.warns = append(c.warns, fmt.Sprintf("thermal_max_c=%.0f hit at %.1fC; clamping level", c.Env.Limits.ThermalMaxC, temp))
	c.mu.Unlock()
	if level > 0.25 {
		return 0.25
	}
	return level
}

func (c *Controller) closeBackends() {
	for _, b := range c.Backends {
		_ = b.Close()
	}
}

func (c *Controller) report() Report {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := make([]string, 0, len(c.Backends))
	for _, b := range c.Backends {
		names = append(names, b.Name())
	}
	return Report{
		Envelope:   c.Env.Name,
		Backends:   names,
		Warnings:   append([]string{}, c.warns...),
		Phases:     append([]string{}, c.phases...),
		FinalLevel: c.level,
	}
}

// OpenBackends selects available backends for this host.
func OpenBackends() (backends []Backend, warnings []string) {
	backends = []Backend{NewDutyCycle(nil)}
	if cg, err := OpenCgroupCPU(""); err != nil {
		warnings = append(warnings, "cgroup: "+err.Error())
	} else if cg != nil {
		backends = append(backends, cg)
	}
	if cf, err := OpenCPUFreq(""); err != nil {
		warnings = append(warnings, "cpufreq: "+err.Error())
	} else if cf != nil {
		backends = append(backends, cf)
	}
	return backends, warnings
}
