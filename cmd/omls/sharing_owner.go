package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// The remote worker must not outlive a vanished coordinator indefinitely.
// EOF cancels immediately; a broken network cancels after the heartbeat lease.
func watchSharingLease(parent context.Context, reader *bufio.Reader, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	beats := make(chan struct{}, 1)
	go func() {
		for {
			line, e := reader.ReadString('\n')
			if e != nil || line != "heartbeat\n" {
				cancel()
				return
			}
			select {
			case beats <- struct{}{}:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				cancel()
				return
			case <-beats:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(timeout)
			}
		}
	}()
	return ctx, cancel
}

// Include process start ticks to avoid mistaking a reused PID for the owner.
func sharingOwnerIdentity(pid int) (string, error) {
	b, e := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if e != nil {
		return "", e
	}
	text := string(b)
	end := strings.LastIndex(text, ")")
	if end < 0 {
		return "", fmt.Errorf("invalid owner process stat")
	}
	fields := strings.Fields(text[end+1:])
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return "", fmt.Errorf("owner process has exited")
	}
	return fields[19], nil
}
func watchSharingOwner(parent context.Context, pid int) (context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(parent)
	if pid < 0 {
		cancel()
		return nil, nil, fmt.Errorf("invalid owner PID")
	}
	if pid == 0 {
		return ctx, cancel, nil
	}
	identity, e := sharingOwnerIdentity(pid)
	if e != nil {
		cancel()
		return nil, nil, e
	}
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, e := sharingOwnerIdentity(pid)
				if e != nil || current != identity {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel, nil
}
