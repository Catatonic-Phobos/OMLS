package main

import (
	"bytes"
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestRemoteResultCompletesWhileHeartbeatWriterIsOpen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sh", "-c", `IFS= read -r document; printf completed-result`)
	var result bytes.Buffer
	if e := sharingRemoteResult(ctx, command, []byte(`{"schema":1}`), &result); e != nil {
		t.Fatal("completed remote job reported a transport failure:", e)
	}
	if result.String() != "completed-result" {
		t.Fatal("wrong result bytes", result.String())
	}
}
func TestRemoteProducerFailureIsNotAccepted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sh", "-c", `IFS= read -r document; printf unfinished; exit 7`)
	if e := sharingRemoteResult(ctx, command, []byte(`{"schema":1}`), &bytes.Buffer{}); e == nil {
		t.Fatal("failed producer was accepted")
	}
}
