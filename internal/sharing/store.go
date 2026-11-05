package sharing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var ErrMiss = errors.New("sharing cache miss")

type Policy struct {
	MaxBytes     int64         `json:"max_bytes"`
	MinFreeBytes uint64        `json:"min_free_bytes"`
	ComputeTTL   time.Duration `json:"compute_ttl_ns"`
}
type Store struct {
	Root   string
	Policy Policy
	now    func() time.Time
}
type Manifest struct {
	Schema   int        `json:"schema"`
	Key      string     `json:"key"`
	Spec     Spec       `json:"spec"`
	SHA256   string     `json:"sha256"`
	Bytes    int64      `json:"bytes"`
	Created  time.Time  `json:"created_at"`
	Accessed time.Time  `json:"last_accessed_at"`
	Expires  *time.Time `json:"expires_at,omitempty"`
	Hits     uint64     `json:"hits"`
}
type Task struct {
	store      *Store
	ID         string
	Spec       Spec
	OutputPath string
	lease      *os.File
	mu         sync.Mutex
	done       bool
}
type pending struct {
	Spec       Spec      `json:"spec"`
	Started    time.Time `json:"started_at"`
	OutputHash string    `json:"output_hash,omitempty"`
}

func Open(ctx context.Context, root string, policy Policy) (*Store, error) {
	if policy.MaxBytes <= 0 || policy.ComputeTTL < 0 {
		return nil, fmt.Errorf("positive cache budget and nonnegative TTL required")
	}
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	s := &Store{Root: root, Policy: policy, now: time.Now}
	for _, dir := range []string{"tmp", "cache/graphics", "cache/compute", "manifests"} {
		if e := os.MkdirAll(filepath.Join(root, dir), 0700); e != nil {
			return nil, e
		}
	}
	unlock, e := s.lock(ctx)
	if e != nil {
		return nil, e
	}
	defer unlock()
	if _, e = s.recoverLocked(); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *Store) SavePolicy(ctx context.Context) error {
	unlock, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	return atomicJSON(filepath.Join(s.Root, "sharing-policy.json"), s.Policy)
}
func (s *Store) lock(ctx context.Context) (func(), error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(s.Root, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	for {
		e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if e == nil {
			return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
		}
		if e != unix.EWOULDBLOCK && e != unix.EAGAIN {
			f.Close()
			return nil, e
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func syncDir(dir string) error {
	f, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func atomicJSON(file string, v any) error {
	f, e := os.CreateTemp(filepath.Dir(file), ".publish-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	e = json.NewEncoder(f).Encode(v)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(f.Name(), file); e != nil {
		return e
	}
	return syncDir(filepath.Dir(file))
}
func hashFile(ctx context.Context, file string) (string, int64, error) {
	f, e := os.Open(file)
	if e != nil {
		return "", 0, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return "", 0, e
	}
	if !st.Mode().IsRegular() {
		return "", 0, fmt.Errorf("cache object is not regular")
	}
	h := sha256.New()
	n, e := copyContext(ctx, h, f, 0)
	return hex.EncodeToString(h.Sum(nil)), n, e
}
func copyContext(ctx context.Context, w io.Writer, r io.Reader, limit int64) (int64, error) {
	buf := make([]byte, 128<<10)
	var total int64
	for {
		if e := ctx.Err(); e != nil {
			return total, e
		}
		n, e := r.Read(buf)
		if n > 0 {
			if limit > 0 && total+int64(n) > limit {
				return total, fmt.Errorf("result exceeds cache budget")
			}
			nw, we := w.Write(buf[:n])
			total += int64(nw)
			if we != nil {
				return total, we
			}
			if nw != n {
				return total, io.ErrShortWrite
			}
		}
		if e == io.EOF {
			return total, nil
		}
		if e != nil {
			return total, e
		}
	}
}
func (s *Store) object(m Manifest) string {
	return filepath.Join(s.Root, "cache", m.Spec.Kind, m.SHA256)
}
func (s *Store) manifest(key string) string { return filepath.Join(s.Root, "manifests", key+".json") }
func (s *Store) readManifest(key string) (Manifest, error) {
	var m Manifest
	if !validHash(key) {
		return m, fmt.Errorf("invalid task key")
	}
	f, e := os.Open(s.manifest(key))
	if e != nil {
		return m, e
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 2<<20))
	d.DisallowUnknownFields()
	if e = d.Decode(&m); e != nil {
		return m, e
	}
	actual, e := m.Spec.Key()
	if e != nil || m.Schema != 1 || m.Key != key || actual != key || !validHash(m.SHA256) || m.Bytes < 0 {
		return m, fmt.Errorf("invalid result manifest")
	}
	return m, nil
}

func (s *Store) Begin(ctx context.Context, spec Spec) (*Task, error) {
	if _, e := spec.Key(); e != nil {
		return nil, e
	}
	unlock, e := s.lock(ctx)
	if e != nil {
		return nil, e
	}
	defer unlock()
	if _, e = s.gcLocked(0); e != nil {
		return nil, e
	}
	var id [16]byte
	if _, e = rand.Read(id[:]); e != nil {
		return nil, e
	}
	name := hex.EncodeToString(id[:])
	dir := filepath.Join(s.Root, "tmp", name)
	if e = os.Mkdir(dir, 0700); e != nil {
		return nil, e
	}
	lease, e := os.OpenFile(filepath.Join(dir, "lease"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		os.RemoveAll(dir)
		return nil, e
	}
	if e = unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		lease.Close()
		os.RemoveAll(dir)
		return nil, e
	}
	if e = atomicJSON(filepath.Join(dir, "task.json"), pending{Spec: spec, Started: s.now()}); e != nil {
		lease.Close()
		os.RemoveAll(dir)
		return nil, e
	}
	return &Task{store: s, ID: name, Spec: spec, OutputPath: filepath.Join(dir, "output.part"), lease: lease}, nil
}
func (t *Task) Abort() error { t.mu.Lock(); defer t.mu.Unlock(); return t.finish() }
func (t *Task) finish() error {
	if t.done {
		return nil
	}
	t.done = true
	e := os.RemoveAll(filepath.Dir(t.OutputPath))
	_ = unix.Flock(int(t.lease.Fd()), unix.LOCK_UN)
	ce := t.lease.Close()
	if e != nil {
		return e
	}
	return ce
}

// Commit is called only after the producer has stopped writing OutputPath.
func (t *Task) Commit(ctx context.Context) (Manifest, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var m Manifest
	if t.done {
		return m, fmt.Errorf("task already closed")
	}
	defer t.finish()
	s := t.store
	sum, n, e := hashFile(ctx, t.OutputPath)
	if e != nil {
		return m, e
	}
	if n > s.Policy.MaxBytes {
		return m, fmt.Errorf("result exceeds cache budget")
	}
	unlock, e := s.lock(ctx)
	if e != nil {
		return m, e
	}
	defer unlock()
	key, _ := t.Spec.Key()
	now := s.now().UTC()
	m = Manifest{Schema: 1, Key: key, Spec: t.Spec, SHA256: sum, Bytes: n, Created: now, Accessed: now}
	if t.Spec.Kind == "compute" && s.Policy.ComputeTTL > 0 {
		exp := now.Add(s.Policy.ComputeTTL)
		m.Expires = &exp
	}
	// Replacing a key with a different result means the producer is nondeterministic.
	if old, err := s.readManifest(key); err == nil && old.SHA256 != sum {
		return Manifest{}, fmt.Errorf("task key already has different output; include all varying inputs in the spec")
	}
	object := s.object(m)
	if e = atomicJSON(filepath.Join(filepath.Dir(t.OutputPath), "task.json"), pending{Spec: t.Spec, Started: now, OutputHash: sum}); e != nil {
		return Manifest{}, e
	}
	// The pending output already occupies disk space. Budget reserves only its
	// future cache footprint; free-space checks do not count those bytes twice.
	extra := n
	if st, err := os.Stat(object); err == nil && st.Mode().IsRegular() {
		extra = n - st.Size()
	}
	if _, e = s.gcLocked(extra); e != nil {
		return Manifest{}, e
	}
	f, e := os.OpenFile(t.OutputPath, os.O_RDWR, 0600)
	if e != nil {
		return Manifest{}, e
	}
	e = f.Sync()
	ce := f.Close()
	if e != nil {
		return Manifest{}, e
	}
	if ce != nil {
		return Manifest{}, ce
	}
	if e = os.Rename(t.OutputPath, object); e != nil {
		return Manifest{}, e
	}
	if e = syncDir(filepath.Dir(object)); e != nil {
		return Manifest{}, e
	}
	if e = atomicJSON(s.manifest(key), m); e != nil {
		return Manifest{}, e
	}
	return m, nil
}

func (s *Store) Put(ctx context.Context, spec Spec, r io.Reader) (Manifest, error) {
	t, e := s.Begin(ctx, spec)
	if e != nil {
		return Manifest{}, e
	}
	defer t.Abort()
	f, e := os.OpenFile(t.OutputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return Manifest{}, e
	}
	_, e = copyContext(ctx, spaceGuard{store: s, writer: f}, r, s.Policy.MaxBytes)
	ce := f.Close()
	if e != nil {
		return Manifest{}, e
	}
	if ce != nil {
		return Manifest{}, ce
	}
	return t.Commit(ctx)
}

// Get holds the cache lock through verification and consumption so GC cannot
// evict an object being returned to a consumer. It never serves partial bytes
// from a corrupt object.
func (s *Store) Get(ctx context.Context, key string, w io.Writer) (Manifest, error) {
	unlock, e := s.lock(ctx)
	if e != nil {
		return Manifest{}, e
	}
	defer unlock()
	m, e := s.readManifest(key)
	if e != nil {
		if !validHash(key) {
			return m, e
		}
		return m, ErrMiss
	}
	if m.Expires != nil && !s.now().Before(*m.Expires) {
		return m, ErrMiss
	}
	sum, n, e := hashFile(ctx, s.object(m))
	if ctx.Err() != nil {
		return m, ctx.Err()
	}
	if e != nil || sum != m.SHA256 || n != m.Bytes {
		return m, ErrMiss
	}
	f, e := os.Open(s.object(m))
	if e != nil {
		return m, e
	}
	defer f.Close()
	if _, e = copyContext(ctx, w, f, 0); e != nil {
		return m, e
	}
	m.Accessed = s.now().UTC()
	m.Hits++
	if e = atomicJSON(s.manifest(key), m); e != nil {
		return m, e
	}
	return m, nil
}

type spaceGuard struct {
	store  *Store
	writer io.Writer
}

func (g spaceGuard) Write(p []byte) (int, error) {
	free, e := g.store.freeBytes()
	if e != nil {
		return 0, e
	}
	if free < g.store.Policy.MinFreeBytes || free-g.store.Policy.MinFreeBytes < uint64(len(p)) {
		return 0, fmt.Errorf("minimum free-space reserve reached")
	}
	return g.writer.Write(p)
}

// Resolve checks before invoking a producer. Producers finish and close their
// output before returning, and must honor cancellation. The lease pins inputs
// from Begin until Commit/Abort, including throughout the producer's work.
func (s *Store) Resolve(ctx context.Context, spec Spec, w io.Writer, producer func(context.Context, string) error) (Manifest, bool, error) {
	key, e := spec.Key()
	if e != nil {
		return Manifest{}, false, e
	}
	m, e := s.Get(ctx, key, w)
	if e == nil {
		return m, true, nil
	}
	if !errors.Is(e, ErrMiss) {
		return m, false, e
	}
	task, e := s.Begin(ctx, spec)
	if e != nil {
		return Manifest{}, false, e
	}
	defer task.Abort()
	if e = producer(ctx, task.OutputPath); e != nil {
		return Manifest{}, false, e
	}
	if e = ctx.Err(); e != nil {
		return Manifest{}, false, e
	}
	if _, e = task.Commit(ctx); e != nil {
		return Manifest{}, false, e
	}
	m, e = s.Get(ctx, key, w)
	return m, false, e
}
