package sharing

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func spec(name string) Spec {
	return Spec{Schema: 1, Kind: "compute", Operation: name, Renderer: "test-producer", Version: "1", Compatibility: "portable-v1", Parameters: []byte(`{"width":128,"options":{"a":1,"b":2}}`)}
}
func openTest(t *testing.T, budget int64) *Store {
	t.Helper()
	s, e := Open(context.Background(), t.TempDir(), Policy{MaxBytes: budget, ComputeTTL: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func putTest(t *testing.T, s *Store, sp Spec, data string) Manifest {
	t.Helper()
	m, e := s.Put(context.Background(), sp, strings.NewReader(data))
	if e != nil {
		t.Fatal(e)
	}
	return m
}

func TestKeyIdentity(t *testing.T) {
	a := spec("render")
	b := a
	b.Parameters = []byte(`{"options":{"b":2,"a":1},"width":128}`)
	ka, e := a.Key()
	if e != nil {
		t.Fatal(e)
	}
	kb, e := b.Key()
	if e != nil || ka != kb {
		t.Fatalf("object order changed task key: %v", e)
	}
	for _, change := range []func(*Spec){func(s *Spec) { s.Version = "2" }, func(s *Spec) { s.Compatibility = "different-driver" }, func(s *Spec) { s.Model = "other-model" }, func(s *Spec) { s.Inputs = []Input{{Name: "scene", SHA256: strings.Repeat("a", 64)}} }, func(s *Spec) { s.Parameters = []byte(`{"width":129}`) }} {
		b = a
		change(&b)
		key, e := b.Key()
		if e != nil || key == ka {
			t.Fatal("incompatible request reused key", e)
		}
	}
	for _, bad := range []string{`null`, `[]`, `{} {}`, `{"a":NaN}`} {
		b = a
		b.Parameters = []byte(bad)
		if _, e := b.Key(); e == nil {
			t.Fatalf("accepted parameters %s", bad)
		}
	}
}

func TestPersistenceCorruptionAndResolve(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, 1024)
	sp := spec("cached-result")
	produced := 0
	producer := func(ctx context.Context, path string) error {
		produced++
		return os.WriteFile(path, []byte("complete result"), 0600)
	}
	var first bytes.Buffer
	m, hit, e := s.Resolve(ctx, sp, &first, producer)
	if e != nil || hit || first.String() != "complete result" {
		t.Fatal(m, hit, e)
	}
	reopened, e := Open(ctx, s.Root, s.Policy)
	if e != nil {
		t.Fatal(e)
	}
	var second bytes.Buffer
	_, hit, e = reopened.Resolve(ctx, sp, &second, producer)
	if e != nil || !hit || produced != 1 || !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("cache did not survive reopen", e)
	}
	if e = os.WriteFile(s.object(m), []byte("corrupt content"), 0600); e != nil {
		t.Fatal(e)
	}
	var consumer bytes.Buffer
	if _, e = s.Get(ctx, m.Key, &consumer); !errors.Is(e, ErrMiss) || consumer.Len() != 0 {
		t.Fatal("corrupt bytes leaked", e)
	}
	consumer.Reset()
	if _, hit, e = s.Resolve(ctx, sp, &consumer, producer); e != nil || hit || produced != 2 {
		t.Fatal("corrupt object not regenerated", e)
	}
	if _, e = s.Put(ctx, sp, strings.NewReader("different output")); e == nil {
		t.Fatal("hidden nondeterminism was silently accepted")
	}
}

func TestTTLGraphicsAndLRU(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, 12)
	clock := time.Now().UTC()
	s.now = func() time.Time { return clock }
	a := putTest(t, s, spec("a"), "aaaa")
	clock = clock.Add(time.Minute)
	bspec := spec("b")
	bspec.Kind = "graphics"
	b := putTest(t, s, bspec, "bbbb")
	clock = clock.Add(time.Minute)
	if _, e := s.Get(ctx, a.Key, io.Discard); e != nil {
		t.Fatal(e)
	}
	clock = clock.Add(time.Minute)
	c := putTest(t, s, spec("c"), "cccc")
	clock = clock.Add(time.Minute)
	_ = putTest(t, s, spec("d"), "dddd")
	if _, e := s.Get(ctx, b.Key, io.Discard); !errors.Is(e, ErrMiss) {
		t.Fatal("LRU victim retained", e)
	}
	for _, key := range []string{a.Key, c.Key} {
		if _, e := s.Get(ctx, key, io.Discard); e != nil {
			t.Fatal("recent result evicted", e)
		}
	}
	s.Policy.MaxBytes = 100
	g := spec("durable-graphics")
	g.Kind = "graphics"
	graphics := putTest(t, s, g, "pixels")
	clock = clock.Add(2 * time.Hour)
	if _, e := s.Get(ctx, a.Key, io.Discard); !errors.Is(e, ErrMiss) {
		t.Fatal("expired compute result returned", e)
	}
	if _, e := s.GC(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Get(ctx, graphics.Key, io.Discard); e != nil {
		t.Fatal("graphics incorrectly got compute TTL", e)
	}
}

func TestActiveInputsArePinnedAndCancellationCleans(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, 8)
	input := putTest(t, s, spec("input"), "12345678")
	running := spec("render-running")
	running.Inputs = []Input{{Name: "asset", SHA256: input.SHA256}}
	task, e := s.Begin(ctx, running)
	if e != nil {
		t.Fatal(e)
	}
	defer task.Abort()
	other, e := Open(ctx, s.Root, s.Policy)
	if e != nil {
		t.Fatal(e)
	}
	status, e := other.Status(ctx)
	if e != nil || status.ActiveTasks != 1 {
		t.Fatal("live task lost at restart", e)
	}
	if _, e := s.Put(ctx, spec("needs-room"), strings.NewReader("abcdefgh")); e == nil {
		t.Fatal("evicted active task input")
	}
	if _, e = s.Get(ctx, input.Key, io.Discard); e != nil {
		t.Fatal(e)
	}
	if e = task.Abort(); e != nil {
		t.Fatal(e)
	}
	_, _, e = s.Resolve(ctx, spec("cancelled"), io.Discard, func(ctx context.Context, path string) error { return context.Canceled })
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	entries, e := os.ReadDir(filepath.Join(s.Root, "tmp"))
	if e != nil || len(entries) != 0 {
		t.Fatal("cancelled temporaries remain", e)
	}
	if _, e = s.Put(ctx, spec("after-release"), strings.NewReader("abcdefgh")); e != nil {
		t.Fatal(e)
	}
}

type blockingWriter struct {
	entered chan struct{}
	release chan struct{}
}

func (w blockingWriter) Write(p []byte) (int, error) {
	close(w.entered)
	<-w.release
	return len(p), nil
}
func TestConsumerProtectedAgainstConcurrentGC(t *testing.T) {
	s := openTest(t, 100)
	m := putTest(t, s, spec("reading"), "payload")
	writer := blockingWriter{make(chan struct{}), make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, e := s.Get(context.Background(), m.Key, writer); done <- e }()
	<-writer.entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, e := s.GC(ctx)
	if !errors.Is(e, context.DeadlineExceeded) {
		close(writer.release)
		t.Fatalf("GC did not wait for reader: %v", e)
	}
	close(writer.release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
}

func TestCrashHelper(t *testing.T) {
	root := os.Getenv("OMLS_TEST_CRASH_ROOT")
	if root == "" {
		return
	}
	s, e := Open(context.Background(), root, Policy{MaxBytes: 1024})
	if e != nil {
		t.Fatal(e)
	}
	task, e := s.Begin(context.Background(), spec("crash"))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(task.OutputPath, []byte("unfinished"), 0600); e != nil {
		t.Fatal(e)
	}
	fmt.Println("task-ready")
	select {}
}
func TestRecoveryAfterKilledProducer(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
	cmd.Env = append(os.Environ(), "OMLS_TEST_CRASH_ROOT="+root)
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	scan := bufio.NewScanner(pipe)
	if !scan.Scan() || scan.Text() != "task-ready" {
		t.Fatal("producer did not start")
	}
	live, e := Open(context.Background(), root, Policy{MaxBytes: 1024})
	if e != nil {
		t.Fatal(e)
	}
	st, e := live.Status(context.Background())
	if e != nil || st.ActiveTasks != 1 {
		t.Fatal("live lease not respected", e)
	}
	if e = cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	_ = cmd.Wait()
	recovered, e := Open(context.Background(), root, Policy{MaxBytes: 1024})
	if e != nil {
		t.Fatal(e)
	}
	dirs, e := os.ReadDir(filepath.Join(root, "tmp"))
	if e != nil || len(dirs) != 0 {
		t.Fatal("abandoned files survived", e)
	}
	st, e = recovered.Status(context.Background())
	if e != nil || st.Objects != 0 || st.Manifests != 0 {
		t.Fatal("incomplete output published", e)
	}
}
