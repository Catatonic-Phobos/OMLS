package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestRemoteLeaseExpiresAndEOFIsImmediate(t *testing.T) {
	for _, closeInput := range []bool{false, true} {
		r, w := io.Pipe()
		ctx, cancel := watchSharingLease(context.Background(), bufio.NewReader(r), 50*time.Millisecond)
		if closeInput {
			_ = w.Close()
		} else {
			if _, e := io.WriteString(w, "heartbeat\n"); e != nil {
				t.Fatal(e)
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("remote task lease survived missing coordinator")
		}
		cancel()
		r.Close()
		w.Close()
	}
}

func TestOwnerExitCancelsProducerAndRemovesTemporaryOutput(t *testing.T) {
	owner := exec.Command("/bin/sleep", "30")
	if e := owner.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = owner.Process.Kill(); _ = owner.Wait() }()
	root := t.TempDir()
	specFile := filepath.Join(root, "spec.json")
	args := []string{"/bin/sh", "-c", `printf unfinished > "$OMLS_RESULT_PATH"; sleep 30`}
	spec := map[string]any{"schema": 1, "kind": "compute", "operation": "owner-cancel-test", "renderer": "shell", "renderer_version": "1", "compatibility": "test", "inputs": []any{}, "parameters": map[string]any{"argv": args}}
	data, _ := json.Marshal(spec)
	if e := os.WriteFile(specFile, data, 0600); e != nil {
		t.Fatal(e)
	}
	command := []string{"run", "--root", filepath.Join(root, "sharing"), "--spec", specFile, "--owner-pid", strconv.Itoa(owner.Process.Pid), "--out", filepath.Join(root, "result"), "--"}
	command = append(command, args...)
	done := make(chan error, 1)
	go func() { done <- runSharing(command) }()
	// Wait for the actual output write, rather than racing process setup.
	deadline := time.Now().Add(5 * time.Second)
	for {
		files, _ := filepath.Glob(filepath.Join(root, "sharing/tmp/*/output.part"))
		if len(files) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("producer did not create output")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e := owner.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	_ = owner.Wait()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancelled producer published a result")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("producer survived owner exit")
	}
	dirs, e := os.ReadDir(filepath.Join(root, "sharing/tmp"))
	if e != nil || len(dirs) != 0 {
		t.Fatal("cancelled output was not removed", e)
	}
	manifests, e := os.ReadDir(filepath.Join(root, "sharing/manifests"))
	if e != nil || len(manifests) != 0 {
		t.Fatal("unfinished result published", e)
	}
}
