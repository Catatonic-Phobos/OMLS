// Package work runs synthetic CPU-bound tasks for OMLS demos.
package work

import (
	"context"
	"time"
)

// Pacer is an optional duty-cycle / throttle hook (Resource Envelope).
type Pacer interface {
	Pace(ctx context.Context) error
}

// Burn runs a CPU-bound loop for roughly `iterations` units of work.
// When pacer is non-nil it is invoked periodically to enforce an envelope.
func Burn(ctx context.Context, iterations int64, pacer Pacer) (duration time.Duration, err error) {
	if iterations < 1 {
		iterations = 1
	}
	start := time.Now()
	var x uint64 = 1
	for i := int64(0); i < iterations; i++ {
		if i&0xffff == 0 {
			select {
			case <-ctx.Done():
				return time.Since(start), ctx.Err()
			default:
			}
			if pacer != nil {
				if err := pacer.Pace(ctx); err != nil {
					return time.Since(start), err
				}
			}
		}
		x = x*1664525 + 1013904223
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
	}
	sink = x
	return time.Since(start), nil
}

// sink prevents the burn loop from being optimized away.
var sink uint64
