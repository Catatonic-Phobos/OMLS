package sharing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type GCReport struct {
	RemovedTemps     int   `json:"removed_temps"`
	RemovedManifests int   `json:"removed_manifests"`
	RemovedObjects   int   `json:"removed_objects"`
	FreedBytes       int64 `json:"freed_bytes"`
	CacheBytes       int64 `json:"cache_bytes"`
}
type activePins struct {
	hashes map[string]bool
	keys   map[string]bool
	active int
}

// flock ownership, rather than a saved PID or clean shutdown marker, decides
// whether a task survived. A killed process releases its lease in the kernel.
func (s *Store) tasksLocked(clean bool) (activePins, int, error) {
	pins := activePins{hashes: map[string]bool{}, keys: map[string]bool{}}
	removed := 0
	entries, e := os.ReadDir(filepath.Join(s.Root, "tmp"))
	if e != nil {
		return pins, 0, e
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(s.Root, "tmp", entry.Name())
		lease, e := os.OpenFile(filepath.Join(dir, "lease"), os.O_RDWR, 0600)
		if e != nil {
			if os.IsNotExist(e) {
				if clean {
					if e = os.RemoveAll(dir); e != nil {
						return pins, removed, e
					}
					removed++
				}
				continue
			}
			return pins, removed, e
		}
		e = unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if e == nil {
			if clean {
				if e = os.RemoveAll(dir); e != nil {
					lease.Close()
					return pins, removed, e
				}
				removed++
			}
			_ = unix.Flock(int(lease.Fd()), unix.LOCK_UN)
			lease.Close()
			continue
		}
		lease.Close()
		if e != unix.EWOULDBLOCK && e != unix.EAGAIN {
			return pins, removed, e
		}
		b, e := os.ReadFile(filepath.Join(dir, "task.json"))
		if e != nil {
			return pins, removed, e
		}
		var task pending
		if e = json.Unmarshal(b, &task); e != nil {
			return pins, removed, e
		}
		key, e := task.Spec.Key()
		if e != nil {
			return pins, removed, e
		}
		pins.keys[key] = true
		pins.active++
		for _, in := range task.Spec.Inputs {
			pins.hashes[in.SHA256] = true
		}
		if validHash(task.OutputHash) {
			pins.hashes[task.OutputHash] = true
		}
	}
	return pins, removed, nil
}

func (s *Store) recoverLocked() (GCReport, error) {
	var result GCReport
	_, n, e := s.tasksLocked(true)
	result.RemovedTemps = n
	if e != nil {
		return result, e
	}
	entries, e := os.ReadDir(filepath.Join(s.Root, "manifests"))
	if e != nil {
		return result, e
	}
	for _, entry := range entries {
		file := filepath.Join(s.Root, "manifests", entry.Name())
		if strings.HasPrefix(entry.Name(), ".publish-") {
			if e = os.Remove(file); e != nil {
				return result, e
			}
			continue
		}
		key := strings.TrimSuffix(entry.Name(), ".json")
		if entry.IsDir() || !validHash(key) || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		m, err := s.readManifest(key)
		if err == nil {
			st, se := os.Stat(s.object(m))
			if se == nil && st.Mode().IsRegular() && st.Size() == m.Bytes {
				continue
			}
		}
		if e = os.Remove(file); e != nil {
			return result, e
		}
		result.RemovedManifests++
	}
	// A crash after object rename but before manifest publication leaves an
	// orphan. Reclaim it on restart without touching a live task's pinned data.
	pins, _, e := s.tasksLocked(false)
	if e != nil {
		return result, e
	}
	referenced := map[string]bool{}
	entries, e = os.ReadDir(filepath.Join(s.Root, "manifests"))
	if e != nil {
		return result, e
	}
	for _, entry := range entries {
		key := strings.TrimSuffix(entry.Name(), ".json")
		if !validHash(key) {
			continue
		}
		m, e := s.readManifest(key)
		if e == nil {
			referenced[s.object(m)] = true
		}
	}
	for _, kind := range []string{"graphics", "compute"} {
		dir := filepath.Join(s.Root, "cache", kind)
		objects, e := os.ReadDir(dir)
		if e != nil {
			return result, e
		}
		for _, entry := range objects {
			if !validHash(entry.Name()) || entry.IsDir() {
				continue
			}
			file := filepath.Join(dir, entry.Name())
			if referenced[file] || pins.hashes[entry.Name()] {
				continue
			}
			st, e := entry.Info()
			if e != nil {
				return result, e
			}
			if e = os.Remove(file); e != nil {
				return result, e
			}
			result.RemovedObjects++
			result.FreedBytes += st.Size()
		}
	}
	return result, nil
}

type objectRecord struct {
	file, hash string
	bytes      int64
	accessed   time.Time
	hits       uint64
	keys       []string
	pinned     bool
}

func (s *Store) freeBytes() (uint64, error) {
	var st unix.Statfs_t
	if e := unix.Statfs(s.Root, &st); e != nil {
		return 0, e
	}
	return st.Bavail * uint64(st.Bsize), nil
}

func (s *Store) gcLocked(incoming int64) (GCReport, error) {
	result, e := s.recoverLocked()
	if e != nil {
		return result, e
	}
	pins, _, e := s.tasksLocked(false)
	if e != nil {
		return result, e
	}
	objects := map[string]*objectRecord{}
	for _, kind := range []string{"graphics", "compute"} {
		dir := filepath.Join(s.Root, "cache", kind)
		entries, e := os.ReadDir(dir)
		if e != nil {
			return result, e
		}
		for _, entry := range entries {
			if entry.IsDir() || !validHash(entry.Name()) {
				continue
			}
			st, e := entry.Info()
			if e != nil {
				return result, e
			}
			if !st.Mode().IsRegular() {
				continue
			}
			file := filepath.Join(dir, entry.Name())
			objects[file] = &objectRecord{file: file, hash: entry.Name(), bytes: st.Size(), accessed: st.ModTime(), pinned: pins.hashes[entry.Name()]}
			result.CacheBytes += st.Size()
		}
	}
	entries, e := os.ReadDir(filepath.Join(s.Root, "manifests"))
	if e != nil {
		return result, e
	}
	for _, entry := range entries {
		key := strings.TrimSuffix(entry.Name(), ".json")
		if !validHash(key) {
			continue
		}
		m, e := s.readManifest(key)
		if e != nil {
			return result, e
		}
		object := objects[s.object(m)]
		if object == nil {
			continue
		}
		if m.Expires != nil && !s.now().Before(*m.Expires) && !pins.keys[key] && !pins.hashes[m.SHA256] {
			if e = os.Remove(s.manifest(key)); e != nil {
				return result, e
			}
			result.RemovedManifests++
			continue
		}
		if len(object.keys) == 0 || m.Accessed.After(object.accessed) {
			object.accessed = m.Accessed
		}
		object.hits += m.Hits
		object.keys = append(object.keys, key)
		if pins.keys[key] {
			object.pinned = true
		}
	}
	var candidates []*objectRecord
	for _, object := range objects {
		if !object.pinned {
			candidates = append(candidates, object)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if (len(a.keys) == 0) != (len(b.keys) == 0) {
			return len(a.keys) == 0
		}
		if !a.accessed.Equal(b.accessed) {
			return a.accessed.Before(b.accessed)
		}
		if a.hits != b.hits {
			return a.hits < b.hits
		}
		return a.file < b.file
	})
	for _, object := range candidates {
		free, e := s.freeBytes()
		if e != nil {
			return result, e
		}
		needed := result.CacheBytes+incoming > s.Policy.MaxBytes || free < s.Policy.MinFreeBytes
		if len(object.keys) > 0 && !needed {
			continue
		}
		for _, key := range object.keys {
			if e = os.Remove(s.manifest(key)); e != nil && !os.IsNotExist(e) {
				return result, e
			}
			result.RemovedManifests++
		}
		if e = os.Remove(object.file); e != nil {
			return result, e
		}
		result.RemovedObjects++
		result.FreedBytes += object.bytes
		result.CacheBytes -= object.bytes
	}
	free, e := s.freeBytes()
	if e != nil {
		return result, e
	}
	if result.CacheBytes+incoming > s.Policy.MaxBytes || free < s.Policy.MinFreeBytes {
		return result, fmt.Errorf("cache capacity exhausted; active task resources remain protected")
	}
	return result, nil
}
func (s *Store) GC(ctx context.Context) (GCReport, error) {
	unlock, e := s.lock(ctx)
	if e != nil {
		return GCReport{}, e
	}
	defer unlock()
	return s.gcLocked(0)
}

type Status struct {
	Root        string `json:"root"`
	Objects     int    `json:"objects"`
	Bytes       int64  `json:"bytes"`
	Manifests   int    `json:"manifests"`
	ActiveTasks int    `json:"active_tasks"`
	FreeBytes   uint64 `json:"filesystem_free_bytes"`
	BudgetBytes int64  `json:"budget_bytes"`
}

func (s *Store) Status(ctx context.Context) (Status, error) {
	unlock, e := s.lock(ctx)
	if e != nil {
		return Status{}, e
	}
	defer unlock()
	v := Status{Root: s.Root, BudgetBytes: s.Policy.MaxBytes}
	pins, _, e := s.tasksLocked(false)
	if e != nil {
		return v, e
	}
	v.ActiveTasks = pins.active
	for _, kind := range []string{"graphics", "compute"} {
		es, e := os.ReadDir(filepath.Join(s.Root, "cache", kind))
		if e != nil {
			return v, e
		}
		for _, entry := range es {
			if validHash(entry.Name()) {
				st, e := entry.Info()
				if e != nil {
					return v, e
				}
				if st.Mode().IsRegular() {
					v.Objects++
					v.Bytes += st.Size()
				}
			}
		}
	}
	es, e := os.ReadDir(filepath.Join(s.Root, "manifests"))
	if e != nil {
		return v, e
	}
	for _, entry := range es {
		if validHash(strings.TrimSuffix(entry.Name(), ".json")) {
			v.Manifests++
		}
	}
	v.FreeBytes, e = s.freeBytes()
	return v, e
}
