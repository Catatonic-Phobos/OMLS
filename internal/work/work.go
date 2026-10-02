// Package work runs synthetic CPU-bound tasks for the OMLS 0.3 demo.
package work

import (
	"context"
	"time"
)

// Burn runs a CPU-bound loop for roughly `iterations` units of work.
// Larger iterations → longer duration. Cancel via ctx.
func Burn(ctx context.Context, iterations int64) (duration time.Duration, err error) {
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
		}
		// Dependent arithmetic so the compiler cannot elide the loop.
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
