package sharing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOrphanPublicationIsRecoveredWithoutDeletingValidContent(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, 1024)
	valid := putTest(t, s, spec("valid"), "complete")
	bytes := []byte("object published before a crash prevented its manifest")
	hash := sha256.Sum256(bytes)
	file := filepath.Join(s.Root, "cache/compute", hex.EncodeToString(hash[:]))
	if e := os.WriteFile(file, bytes, 0600); e != nil {
		t.Fatal(e)
	}
	recovered, e := Open(ctx, s.Root, s.Policy)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(file); !os.IsNotExist(e) {
		t.Fatal("orphan was retained", e)
	}
	if _, e = recovered.Get(ctx, valid.Key, io.Discard); e != nil {
		t.Fatal("valid result lost in recovery", e)
	}
}

func TestSharedObjectSurvivesExpiryOfOnlyOneReference(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, 1024)
	clock := time.Now().UTC()
	s.now = func() time.Time { return clock }
	first := putTest(t, s, spec("older-reference"), "same content")
	clock = clock.Add(30 * time.Minute)
	second := putTest(t, s, spec("newer-reference"), "same content")
	if first.SHA256 != second.SHA256 {
		t.Fatal("content not deduplicated")
	}
	status, e := s.Status(ctx)
	if e != nil || status.Objects != 1 || status.Manifests != 2 {
		t.Fatal(status, e)
	}
	clock = clock.Add(40 * time.Minute)
	if _, e = s.GC(ctx); e != nil {
		t.Fatal(e)
	}
	status, e = s.Status(ctx)
	if e != nil || status.Objects != 1 || status.Manifests != 1 {
		t.Fatal("live reference lost with expired one", status, e)
	}
	if _, e = s.Get(ctx, second.Key, io.Discard); e != nil {
		t.Fatal(e)
	}
}
