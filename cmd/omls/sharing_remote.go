package main

import (
	"context"
	"io"
	"os/exec"
	"time"
)

func sharingRemoteResult(ctx context.Context, c *exec.Cmd, document []byte, w io.Writer) error {
	input, feed := io.Pipe()
	defer input.Close()
	defer feed.Close()
	c.Stdin = input
	stream, e := c.StdoutPipe()
	if e != nil {
		return e
	}
	defer stream.Close()
	c.WaitDelay = time.Second
	if e = c.Start(); e != nil {
		return e
	}
	done := make(chan struct{})
	go func() {
		defer feed.Close()
		if _, e := feed.Write(append(document, '\n')); e != nil {
			return
		}
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, e := io.WriteString(feed, "heartbeat\n"); e != nil {
					return
				}
			}
		}
	}()
	_, copyErr := io.Copy(w, stream)
	close(done)
	// Closing the writer produces EOF for exec's stdin copier. Closing its
	// reader here would turn an otherwise successful job into ErrClosedPipe.
	_ = feed.Close()
	if copyErr != nil {
		_ = c.Process.Kill()
	}
	waitErr := c.Wait()
	if copyErr != nil {
		return copyErr
	}
	return waitErr
}
