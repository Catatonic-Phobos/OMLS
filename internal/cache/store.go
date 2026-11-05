// Package cache keeps a fixed RAM reservation on a node and serves byte ranges
// back to the peer that placed them (OMLS 1.3).
package cache

import (
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	// KindMemory is a slice of resident process memory.
	KindMemory = "memory"
	// KindGraphics is a slice of the displayed frame.
	KindGraphics = "graphics"

	// DefaultListen is the TCP port every node uses for the RAM cache.
	DefaultListen = ":7444"
	// MaxReserve is the largest reservation a node accepts.
	MaxReserve = 1 << 30
	// DefaultReserve is the RAM held on the peer when place omits --bytes.
	DefaultReserve = 512 << 20
	// DefaultFill is how much of that reservation place copies.
	DefaultFill = 32 << 20
	// Chunk is the largest single write or read on the wire.
	Chunk = 1 << 20
)

// Stat is the live reservation.
type Stat struct {
	Kind     string `json:"kind"`
	Capacity int    `json:"capacity"`
	Used     int    `json:"used"`
	Locked   bool   `json:"locked"`
	Source   string `json:"source"`
	Hostname string `json:"hostname"`
}

// Store is one reservation inside this process.
type Store struct {
	mu       sync.Mutex
	buf      []byte
	used     int
	kind     string
	source   string
	locked   bool
	hostname string
	slots    map[string]*slot
}

type slot struct {
	buf    []byte
	used   int
	kind   string
	source string
	locked bool
}

// NewStore starts with no reservation.
func NewStore() *Store {
	host, _ := os.Hostname()
	return &Store{hostname: host, slots: map[string]*slot{}}
}

// Reserve replaces any previous reservation with n bytes of RAM.
// The block is locked when the kernel allows it. n is refused when it is
// above MaxReserve or above half of MemAvailable.
func (s *Store) Reserve(n int, kind, source string) (Stat, error) {
	if kind != KindMemory && kind != KindGraphics {
		return Stat{}, fmt.Errorf("kind must be %s or %s", KindMemory, KindGraphics)
	}
	if n < 1 {
		return Stat{}, fmt.Errorf("reservation must be at least 1 byte")
	}
	if n > MaxReserve {
		return Stat{}, fmt.Errorf("reservation %d exceeds max %d", n, MaxReserve)
	}
	if err := s.checkRoom("", n); err != nil {
		return Stat{}, err
	}
	buf := make([]byte, n)
	locked := unix.Mlock(buf) == nil
	s.mu.Lock()
	prev := s.buf
	s.buf = buf
	s.used = 0
	s.kind = kind
	s.source = source
	s.locked = locked
	st := s.statLocked()
	s.mu.Unlock()
	if len(prev) > 0 {
		_ = unix.Munlock(prev)
	}
	return st, nil
}

// WriteAt copies p into the reservation at offset.
func (s *Store) WriteAt(offset int, p []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buf == nil {
		return fmt.Errorf("no reservation")
	}
	if offset < 0 || offset+len(p) > len(s.buf) {
		return fmt.Errorf("write [%d,%d) outside reservation of %d", offset, offset+len(p), len(s.buf))
	}
	copy(s.buf[offset:], p)
	if end := offset + len(p); end > s.used {
		s.used = end
	}
	return nil
}

// ReadAt returns length bytes at offset.
func (s *Store) ReadAt(offset, length int) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buf == nil {
		return nil, fmt.Errorf("no reservation")
	}
	if offset < 0 || length < 0 || offset+length > len(s.buf) {
		return nil, fmt.Errorf("read [%d,%d) outside reservation of %d", offset, offset+length, len(s.buf))
	}
	out := make([]byte, length)
	copy(out, s.buf[offset:offset+length])
	return out, nil
}

// Release drops the reservation and unlocks it.
func (s *Store) Release() Stat {
	s.mu.Lock()
	prev := s.buf
	s.buf = nil
	s.used = 0
	s.kind = ""
	s.source = ""
	s.locked = false
	st := s.statLocked()
	s.mu.Unlock()
	if len(prev) > 0 {
		_ = unix.Munlock(prev)
	}
	return st
}

// Stat returns the current reservation.
func (s *Store) Stat() Stat {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statLocked()
}

func (s *Store) statLocked() Stat {
	return Stat{
		Kind:     s.kind,
		Capacity: len(s.buf),
		Used:     s.used,
		Locked:   s.locked && len(s.buf) > 0,
		Source:   s.source,
		Hostname: s.hostname,
	}
}

// ReserveNamed holds one named slice alongside the legacy reservation.
// A second call with the same name replaces that slice. The name is the
// requester plus the function, so two machines do not overwrite each other.
func (s *Store) ReserveNamed(name string, n int, kind, source string) (Stat, error) {
	if name == "" {
		return s.Reserve(n, kind, source)
	}
	if kind != KindMemory && kind != KindGraphics {
		return Stat{}, fmt.Errorf("kind must be %s or %s", KindMemory, KindGraphics)
	}
	if n < 1 {
		return Stat{}, fmt.Errorf("reservation must be at least 1 byte")
	}
	if n > MaxReserve {
		return Stat{}, fmt.Errorf("reservation %d exceeds max %d", n, MaxReserve)
	}
	if err := s.checkRoom(name, n); err != nil {
		return Stat{}, err
	}
	buf := make([]byte, n)
	locked := unix.Mlock(buf) == nil
	s.mu.Lock()
	if s.slots == nil {
		s.slots = map[string]*slot{}
	}
	prev := s.slots[name]
	s.slots[name] = &slot{buf: buf, kind: kind, source: source, locked: locked}
	st := s.slotStatLocked(name)
	s.mu.Unlock()
	if prev != nil && len(prev.buf) > 0 {
		_ = unix.Munlock(prev.buf)
	}
	return st, nil
}

// WriteNamed copies p into the named reservation at offset.
func (s *Store) WriteNamed(name string, offset int, p []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := s.slots[name]
	if sl == nil || sl.buf == nil {
		return fmt.Errorf("no reservation")
	}
	if offset < 0 || offset+len(p) > len(sl.buf) {
		return fmt.Errorf("write [%d,%d) outside reservation of %d", offset, offset+len(p), len(sl.buf))
	}
	copy(sl.buf[offset:], p)
	if end := offset + len(p); end > sl.used {
		sl.used = end
	}
	return nil
}

// ReadNamed returns length bytes at offset from the named reservation.
func (s *Store) ReadNamed(name string, offset, length int) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := s.slots[name]
	if sl == nil || sl.buf == nil {
		return nil, fmt.Errorf("no reservation")
	}
	if offset < 0 || length < 0 || offset+length > len(sl.buf) {
		return nil, fmt.Errorf("read [%d,%d) outside reservation of %d", offset, offset+length, len(sl.buf))
	}
	out := make([]byte, length)
	copy(out, sl.buf[offset:offset+length])
	return out, nil
}

// StatNamed returns one named reservation. An unknown name is an empty stat.
func (s *Store) StatNamed(name string) Stat {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.slotStatLocked(name)
}

// ReleaseNamed drops one named reservation.
func (s *Store) ReleaseNamed(name string) Stat {
	s.mu.Lock()
	prev := s.slots[name]
	delete(s.slots, name)
	st := s.slotStatLocked(name)
	s.mu.Unlock()
	if prev != nil && len(prev.buf) > 0 {
		_ = unix.Munlock(prev.buf)
	}
	return st
}

func (s *Store) slotStatLocked(name string) Stat {
	sl := s.slots[name]
	if sl == nil {
		return Stat{Hostname: s.hostname}
	}
	return Stat{
		Kind:     sl.kind,
		Capacity: len(sl.buf),
		Used:     sl.used,
		Locked:   sl.locked && len(sl.buf) > 0,
		Source:   sl.source,
		Hostname: s.hostname,
	}
}

func (s *Store) checkRoom(replacing string, n int) error {
	avail := memAvailable()
	if avail <= 0 {
		return nil
	}
	s.mu.Lock()
	used := s.occupiedLocked(replacing)
	s.mu.Unlock()
	if int64(used+n) > avail/2 {
		return fmt.Errorf("reservation %d exceeds half of available RAM (%d)", used+n, avail)
	}
	return nil
}

func (s *Store) occupiedLocked(replacing string) int {
	n := 0
	if replacing != "" {
		n += len(s.buf)
	}
	for name, sl := range s.slots {
		if name == replacing || sl == nil {
			continue
		}
		n += len(sl.buf)
	}
	return n
}

func memAvailable() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	var key string
	var kb int64
	for {
		_, err := fmt.Fscan(f, &key, &kb)
		if err != nil {
			return 0
		}
		var skip string
		_, _ = fmt.Fscan(f, &skip)
		if key == "MemAvailable:" {
			return kb * 1024
		}
	}
}
