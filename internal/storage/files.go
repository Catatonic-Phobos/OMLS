package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type Result struct {
	Bytes, Downloaded int64
	Hash              string
	Hit               bool
	Duration          time.Duration
}

func (p Profile) remote(relative string) (string, error) {
	if relative == "" || strings.HasPrefix(relative, "/") || strings.ContainsAny(relative, "\x00\r\n\\") {
		return "", fmt.Errorf("path must be a relative file path")
	}
	for _, part := range strings.Split(relative, "/") {
		if part == ".." {
			return "", fmt.Errorf("parent traversal is not allowed")
		}
	}
	relative = path.Clean(relative)
	if relative == "." {
		return "", fmt.Errorf("path must name a file")
	}
	return path.Join(p.Root, relative), nil
}
func digest(file string) (string, int64, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	if !st.Mode().IsRegular() {
		return "", 0, fmt.Errorf("source must be a regular file")
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}
func (p Profile) remoteHash(ctx context.Context, file string) (string, error) {
	b, err := p.ssh(ctx, "sha256sum -- "+quote(file)).Output()
	if err != nil {
		return "", fmt.Errorf("remote SHA-256 failed: %w", err)
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return "", fmt.Errorf("remote SHA-256 is missing")
	}
	v := fields[0]
	if len(v) != 64 {
		return "", fmt.Errorf("invalid remote SHA-256")
	}
	if _, err := hex.DecodeString(v); err != nil {
		return "", fmt.Errorf("invalid remote SHA-256")
	}
	return v, nil
}
func smbQuote(s string) (string, error) {
	if strings.ContainsAny(s, "\";\r\n\x00") {
		return "", fmt.Errorf("SMB paths cannot contain quotes, semicolons or control characters")
	}
	return "\"" + s + "\"", nil
}
func (p Profile) download(ctx context.Context, remote, local string) error {
	if p.Mode == "ssh" {
		f, err := os.OpenFile(local, os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		c := p.ssh(ctx, "cat -- "+quote(remote))
		c.Stdout = f
		err = c.Run()
		ce := f.Close()
		if err != nil {
			return fmt.Errorf("SSH download failed: %w", err)
		}
		return ce
	}
	r, err := smbQuote(remote)
	if err != nil {
		return err
	}
	l, err := smbQuote(local)
	if err != nil {
		return err
	}
	c, clean, err := p.smb(ctx, "get "+r+" "+l)
	if err != nil {
		return err
	}
	defer clean()
	if err := c.Run(); err != nil {
		return fmt.Errorf("SMB download failed: %w", err)
	}
	return nil
}

// Get validates remote contents and materializes an ordinary local file.
// A consumer must explicitly read that output; no process mappings are changed.
func (p Profile) Get(ctx context.Context, relative, output, cacheDir string) (result Result, err error) {
	start := time.Now()
	defer func() { result.Duration = time.Since(start) }()
	if output == "" {
		return result, fmt.Errorf("--out is required")
	}
	remote, err := p.remote(relative)
	if err != nil {
		return result, err
	}
	if err = os.MkdirAll(cacheDir, 0700); err != nil {
		return result, err
	}
	expected := ""
	if p.Mode == "ssh" {
		expected, err = p.remoteHash(ctx, remote)
		if err != nil {
			return result, err
		}
		object := filepath.Join(cacheDir, expected)
		if h, n, e := digest(object); e == nil && h == expected {
			result.Hash = h
			result.Bytes = n
			result.Hit = true
			err = materialize(object, output)
			return result, err
		}
	}
	f, err := os.CreateTemp(cacheDir, ".download-*")
	if err != nil {
		return result, err
	}
	tmp := f.Name()
	f.Close()
	defer os.Remove(tmp)
	if err = p.download(ctx, remote, tmp); err != nil {
		return result, err
	}
	h, n, err := digest(tmp)
	if err != nil {
		return result, err
	}
	result.Downloaded = n
	if expected != "" && h != expected {
		return result, fmt.Errorf("source changed during transfer or integrity check failed; retry")
	}
	result.Hash = h
	result.Bytes = n
	object := filepath.Join(cacheDir, h)
	if err = os.Rename(tmp, object); err != nil {
		return result, err
	}
	err = materialize(object, output)
	return result, err
}
func materialize(source, output string) error {
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(output), ".omls-output-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	ce := out.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	return os.Rename(out.Name(), output)
}

// Put stages, reads back or hashes, and atomically publishes a real file.
func (p Profile) Put(ctx context.Context, source, relative string) error {
	remote, err := p.remote(relative)
	if err != nil {
		return err
	}
	h, n, err := digest(source)
	if err != nil {
		return err
	}
	var id [12]byte
	if _, err = rand.Read(id[:]); err != nil {
		return err
	}
	stage := remote + ".omls-" + hex.EncodeToString(id[:])
	if p.Mode == "ssh" {
		f, err := os.Open(source)
		if err != nil {
			return err
		}
		defer f.Close()
		c := p.ssh(ctx, "umask 077; mkdir -p -- "+quote(path.Dir(remote))+" && cat > "+quote(stage))
		c.Stdin = f
		if err = c.Run(); err != nil {
			return fmt.Errorf("SSH upload failed: %w", err)
		}
		// Remove only this operation's private staging file, including on cancellation.
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_ = p.ssh(cleanup, "rm -f -- "+quote(stage)).Run()
		}()
		got, err := p.remoteHash(ctx, stage)
		if err != nil {
			return err
		}
		if got != h {
			return fmt.Errorf("uploaded file failed integrity check")
		}
		if err = p.ssh(ctx, "mv -f -- "+quote(stage)+" "+quote(remote)).Run(); err != nil {
			return fmt.Errorf("publish failed: %w", err)
		}
	} else {
		// smbclient cannot create a directory tree atomically; require its parent.
		src, err := filepath.Abs(source)
		if err != nil {
			return err
		}
		s, err := smbQuote(src)
		if err != nil {
			return err
		}
		r, err := smbQuote(stage)
		if err != nil {
			return err
		}
		dst, err := smbQuote(remote)
		if err != nil {
			return err
		}
		run := func(command string) error {
			c, clean, e := p.smb(ctx, command)
			if e != nil {
				return e
			}
			defer clean()
			return c.Run()
		}
		if err = run("put " + s + " " + r); err != nil {
			return fmt.Errorf("SMB upload failed (remote parent must exist): %w", err)
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			c, clean, e := p.smb(cleanup, "del "+r)
			if e == nil {
				defer clean()
				_ = c.Run()
			}
		}()
		f, err := os.CreateTemp("", "omls-verify-*")
		if err != nil {
			return err
		}
		f.Close()
		defer os.Remove(f.Name())
		if err = p.download(ctx, stage, f.Name()); err != nil {
			return err
		}
		got, _, err := digest(f.Name())
		if err != nil {
			return err
		}
		if got != h {
			return fmt.Errorf("SMB readback integrity check failed")
		}
		if err = run("rename " + r + " " + dst); err != nil {
			return fmt.Errorf("SMB publish failed (destination may already exist): %w", err)
		}
	}
	fmt.Printf("uploaded bytes=%d sha256=%s node=%s protocol=%s verified=yes\n", n, h, p.Host, p.Mode)
	return nil
}
