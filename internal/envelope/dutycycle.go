package envelope

import (
	"context"
	"sync/atomic"
	"time"
)

// DutyCycle limits CPU burn in userspace by sleeping within a fixed period.
// It always works without privileges and is the baseline 0.4 backend.
type DutyCycle struct {
	period time.Duration
	level  atomic.Uint64 // float64 bits
	ran    time.Duration
	window time.Time
}

// NewDutyCycle creates a userspace duty-cycle backend. period defaults to 50ms.
func NewDutyCycle(period *time.Duration) *DutyCycle {
	p := 50 * time.Millisecond
	if period != nil && *period > 0 {
		p = *period
	}
	d := &DutyCycle{period: p, window: time.Now()}
	d.level.Store(mathFloatBits(1))
	return d
}

func (d *DutyCycle) Name() string { return "duty-cycle" }

func (d *DutyCycle) SetLevel(ctx context.Context, level float64) error {
	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	d.level.Store(mathFloatBits(level))
	return nil
}

func (d *DutyCycle) Close() error { return nil }

// Level returns the current duty cycle.
func (d *DutyCycle) Level() float64 {
	return mathFloatFromBits(d.level.Load())
}

// Pace enforces the duty cycle. Call between burn slices.
func (d *DutyCycle) Pace(ctx context.Context) error {
	level := d.Level()
	now := time.Now()
	if d.window.IsZero() {
		d.window = now
	}
	if now.Sub(d.window) >= d.period {
		d.window = now
		d.ran = 0
	}
	onBudget := time.Duration(float64(d.period) * level)
	if level <= 0 {
		// Sleep a slice and re-check.
		return sleepCtx(ctx, d.period/4)
	}
	if d.ran >= onBudget {
		off := d.period - now.Sub(d.window)
		if off < time.Millisecond {
			off = time.Millisecond
		}
		if err := sleepCtx(ctx, off); err != nil {
			return err
		}
		d.window = time.Now()
		d.ran = 0
		return nil
	}
	// Account a small quantum as "ran" when Pace is polled from the burn loop.
	d.ran += time.Millisecond
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func mathFloatBits(f float64) uint64 {
	return uint64(f * 1_000_000)
}

func mathFloatFromBits(u uint64) float64 {
	return float64(u) / 1_000_000
}
