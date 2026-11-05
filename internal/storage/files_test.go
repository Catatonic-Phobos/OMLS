package storage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSSHFileCacheLifecycle(t *testing.T) {
	// Execute the remote shell commands against a temporary filesystem. This
	// exercises quoting, integrity, invalidation and publication without LAN keys.
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\nfor arg do last=$arg; done\nexec /bin/sh -c \"$last\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	p := Profile{Mode: "ssh", Root: filepath.Join(dir, "peer"), Host: "studio", User: "testuser", Key: "/unused", Port: 22}
	ctx := context.Background()
	source := filepath.Join(dir, "game-file")
	payload := []byte("asset data from an application\n")
	if err := os.WriteFile(source, payload, 0600); err != nil {
		t.Fatal(err)
	}
	remote := "assets/a file's ; echo sentinel"
	out := filepath.Join(dir, "consumer", "asset")
	cache := filepath.Join(dir, "cache")
	if err := p.Put(ctx, source, remote); err != nil {
		t.Fatal(err)
	}
	first, err := p.Get(ctx, remote, out, cache)
	if err != nil {
		t.Fatal(err)
	}
	if first.Hit || first.Downloaded != int64(len(payload)) {
		t.Fatalf("first: %+v", first)
	}
	got, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("consumer contents: %q %v", got, err)
	}
	second, err := p.Get(ctx, remote, out, cache)
	if err != nil || !second.Hit || second.Downloaded != 0 {
		t.Fatalf("hit: %+v %v", second, err)
	}
	if err := os.WriteFile(filepath.Join(cache, first.Hash), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	repaired, err := p.Get(ctx, remote, out, cache)
	if err != nil || repaired.Hit {
		t.Fatalf("corrupt cache was trusted: %+v %v", repaired, err)
	}
	if err := os.WriteFile(filepath.Join(p.Root, remote), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := p.Get(ctx, remote, out, cache)
	if err != nil || changed.Hash == first.Hash || changed.Hit {
		t.Fatalf("stale cache: %+v %v", changed, err)
	}
	if err := os.Remove(filepath.Join(p.Root, remote)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(ctx, remote, out, cache); err == nil {
		t.Fatal("unavailable source silently used stale content")
	}
	for _, bad := range []string{"../escape", "/absolute", "assets/../../escape", "x\ny", "."} {
		if _, err := p.remote(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestPrivateProfilesAndLiteralValues(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "connections.env")
	if err := Init(file, dir); err != nil {
		t.Fatal(err)
	}
	p, err := Load(file, "studio")
	if err != nil || p.Root != "" {
		t.Fatalf("profile %+v %v", p, err)
	}
	if err := Init(file, dir); err == nil {
		t.Fatal("overwrote existing profile")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte("OMLS_STUDIO_PASSWORD="), []byte("OMLS_STUDIO_PASSWORD=$(do-not-execute)"), 1)
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	p, err = Load(file, "STUDIO")
	if err != nil || p.Password != "$(do-not-execute)" {
		t.Fatalf("literal handling: %v", err)
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file, "STUDIO"); err == nil {
		t.Fatal("accepted world-readable secrets")
	}
	if _, err := smbQuote("foo;del bar"); err == nil {
		t.Fatal("accepted SMB command separator")
	}
	if !strings.Contains(quote("a'b"), "'\"'\"'") {
		t.Fatal("shell quote failed")
	}
}
