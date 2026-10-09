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
}

// NewStore starts with no reservation.
func NewStore() *Store {
	host, _ := os.Hostname()
	return &Store{hostname: host}
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
	avail := memAvailable()
	if avail > 0 && int64(n) > avail/2 {
		return Stat{}, fmt.Errorf("reservation %d exceeds half of available RAM (%d)", n, avail)
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
