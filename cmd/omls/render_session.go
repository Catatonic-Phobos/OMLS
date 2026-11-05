package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/renderstream"
	"github.com/Catatonic-Phobos/OMLS/internal/sharing"
	"github.com/Catatonic-Phobos/OMLS/internal/storage"
)

func runRenderSession(ctx context.Context, p storage.Profile, binary, icd, expected, root, socket string, trace bool) error {
	policy := sharing.Policy{MaxBytes: 4 << 30, MinFreeBytes: 1 << 30, ComputeTTL: 24 * time.Hour}
	if b, e := os.ReadFile(filepath.Join(root, "sharing-policy.json")); e == nil {
		if e = json.Unmarshal(b, &policy); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	store, e := sharing.Open(ctx, root, policy)
	if e != nil {
		return e
	}
	params, _ := json.Marshal(map[string]any{"executor": p.Host, "icd": icd, "expected_device": expected, "trace": trace})
	task, e := store.Begin(ctx, sharing.Spec{Schema: 1, Kind: "graphics", Operation: "graphics-session", Renderer: "gfxstream", Version: "94240f814c0097a63027ae680ea4bec7b0cb25f7", Compatibility: "omgr-1", Parameters: params})
	if e != nil {
		return e
	}
	defer task.Abort()
	if socket == "" {
		socket = filepath.Join(filepath.Dir(task.OutputPath), "channel.sock")
	}
	if e = os.MkdirAll(filepath.Dir(socket), 0700); e != nil {
		return e
	}
	listener, e := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if e != nil {
		return e
	}
	defer listener.Close()
	if e = os.Chmod(socket, 0600); e != nil {
		return e
	}
	command := []string{binary, "--mux", "--icd", icd}
	if trace {
		command = append(command, "--trace")
	}
	if expected != "" {
		command = append(command, "--expect", expected)
	}
	remote, e := p.Exec(ctx, command...)
	if e != nil {
		return e
	}
	remote.Stderr = os.Stderr
	return renderstream.Run(ctx, remote, listener, func(b []byte) error {
		var report renderProbe
		if e := json.Unmarshal(b, &report); e != nil {
			return e
		}
		if e := validateRenderSession(report, expected); e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"socket": socket, "task_id": task.ID, "executor": p.Host, "backend": report})
	})
}
