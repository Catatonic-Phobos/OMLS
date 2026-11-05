package renderstream

import (
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type link struct {
	conn      net.Conn
	opened    bool
	ack       chan struct{}
	done      chan struct{}
	replies   chan []byte
	once      sync.Once
	closeSent atomic.Bool
}

func (l *link) stop() { l.once.Do(func() { close(l.done); _ = l.conn.Close() }) }

type Session struct {
	Input    io.WriteCloser
	Output   io.ReadCloser
	Listener net.Listener
	OnReady  func([]byte) error
	mu       sync.Mutex
	writer   sync.Mutex
	links    map[uint32]*link
	next     uint32
	done     chan struct{}
	once     sync.Once
	failure  error
	workers  sync.WaitGroup
	lastRead atomic.Int64
}

// Serve owns the listener and transport. Each accepted local stream becomes a
// separate decoder channel, sharing the same remote renderer/device process.
func (s *Session) Serve(ctx context.Context) error {
	s.links = map[uint32]*link{}
	s.done = make(chan struct{})
	s.lastRead.Store(time.Now().UnixNano())
	defer func() { s.stop(nil); s.workers.Wait() }()
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		select {
		case <-ctx.Done():
			s.stop(ctx.Err())
		case <-s.done:
		}
	}()
	// The startup deadline also bounds a peer which never produces Ready.
	startTimer := time.AfterFunc(time.Minute, func() { s.stop(fmt.Errorf("graphics session startup timed out")) })
	defer startTimer.Stop()
	first, e := Read(s.Output)
	if e != nil {
		return e
	}
	if first.Kind != Ready || first.Channel != 0 {
		return fmt.Errorf("missing graphics session handshake")
	}
	if s.OnReady != nil {
		if e = s.OnReady(first.Payload); e != nil {
			return e
		}
	}
	startTimer.Stop()
	s.lastRead.Store(time.Now().UnixNano())
	s.workers.Add(3)
	go s.receive()
	go s.keepAlive()
	go s.watchdog()
	for {
		conn, e := s.Listener.Accept()
		if e != nil {
			select {
			case <-s.done:
				s.mu.Lock()
				failure := s.failure
				s.mu.Unlock()
				return failure
			default:
				return e
			}
		}
		s.mu.Lock()
		select {
		case <-s.done:
			s.mu.Unlock()
			conn.Close()
			continue
		default:
		}
		if len(s.links) >= MaxChannels || s.next == ^uint32(0) {
			s.mu.Unlock()
			conn.Close()
			continue
		}
		s.next++
		id := s.next
		l := &link{conn: conn, ack: make(chan struct{}), done: make(chan struct{}), replies: make(chan []byte, 4)}
		s.links[id] = l
		s.mu.Unlock()
		s.workers.Add(2)
		go s.forward(id, l)
		go s.deliver(id, l)
	}
}
func (s *Session) stop(e error) {
	s.once.Do(func() {
		s.mu.Lock()
		s.failure = e
		close(s.done)
		links := make([]*link, 0, len(s.links))
		for _, l := range s.links {
			links = append(links, l)
		}
		s.mu.Unlock()
		_ = s.Listener.Close()
		_ = s.Input.Close()
		_ = s.Output.Close()
		for _, l := range links {
			l.stop()
		}
	})
}
func (s *Session) requestClose(id uint32, l *link) {
	if l.closeSent.CompareAndSwap(false, true) {
		_ = s.send(Frame{Kind: Close, Channel: id})
	}
}
func (s *Session) send(f Frame) error {
	s.writer.Lock()
	defer s.writer.Unlock()
	select {
	case <-s.done:
		return io.ErrClosedPipe
	default:
	}
	e := Write(s.Input, f)
	if e != nil {
		s.stop(e)
	}
	return e
}
func (s *Session) forward(id uint32, l *link) {
	defer s.workers.Done()
	if e := s.send(Frame{Kind: Open, Channel: id}); e != nil {
		return
	}
	select {
	case <-l.ack:
	case <-l.done:
		return
	case <-s.done:
		return
	}
	buffer := make([]byte, 64<<10)
	for {
		n, readErr := l.conn.Read(buffer)
		if n > 0 {
			if e := s.send(Frame{Kind: Data, Channel: id, Payload: buffer[:n]}); e != nil {
				return
			}
		}
		if readErr != nil {
			select {
			case <-l.done:
				return
			case <-s.done:
				return
			default:
			}
			s.requestClose(id, l)
			return
		}
	}
}
func (s *Session) deliver(id uint32, l *link) {
	defer s.workers.Done()
	for {
		select {
		case <-s.done:
			return
		case <-l.done:
			return
		case reply := <-l.replies:
			_ = l.conn.SetWriteDeadline(time.Now().Add(25 * time.Second))
			if e := writeAll(l.conn, reply); e != nil {
				l.stop()
				s.requestClose(id, l)
				return
			}
		}
	}
}
func (s *Session) receive() {
	defer s.workers.Done()
	for {
		f, e := Read(s.Output)
		if e != nil {
			s.stop(e)
			return
		}
		s.lastRead.Store(time.Now().UnixNano())
		if f.Kind == Pong && f.Channel == 0 && len(f.Payload) == 0 {
			continue
		}
		if f.Kind == Failure {
			s.stop(fmt.Errorf("remote graphics session: %s", f.Payload))
			return
		}
		s.mu.Lock()
		l := s.links[f.Channel]
		if l == nil {
			s.mu.Unlock()
			s.stop(fmt.Errorf("unknown graphics reply channel"))
			return
		}
		switch f.Kind {
		case Opened:
			if l.opened || len(f.Payload) != 0 {
				s.mu.Unlock()
				s.stop(fmt.Errorf("invalid graphics open acknowledgement"))
				return
			}
			l.opened = true
			close(l.ack)
			s.mu.Unlock()
		case Reply:
			if !l.opened {
				s.mu.Unlock()
				s.stop(fmt.Errorf("reply before channel acknowledgement"))
				return
			}
			s.mu.Unlock()
			select {
			case l.replies <- f.Payload:
			case <-l.done:
			case <-s.done:
				return
			}
		case Closed:
			if len(f.Payload) != 0 {
				s.mu.Unlock()
				s.stop(fmt.Errorf("invalid graphics close acknowledgement"))
				return
			}
			delete(s.links, f.Channel)
			s.mu.Unlock()
			l.stop()
		default:
			s.mu.Unlock()
			s.stop(fmt.Errorf("invalid graphics reply type"))
			return
		}
	}
}
func (s *Session) keepAlive() {
	defer s.workers.Done()
	timer := time.NewTicker(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-timer.C:
			if e := s.send(Frame{Kind: Ping}); e != nil {
				return
			}
		}
	}
}
func (s *Session) watchdog() {
	defer s.workers.Done()
	timer := time.NewTicker(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-timer.C:
			if time.Since(time.Unix(0, s.lastRead.Load())) > 25*time.Second {
				s.stop(fmt.Errorf("graphics session heartbeat expired"))
				return
			}
		}
	}
}

// Run starts exactly one authenticated host command for a complete session.
func Run(ctx context.Context, command *exec.Cmd, listener net.Listener, ready func([]byte) error) error {
	input, e := command.StdinPipe()
	if e != nil {
		return e
	}
	output, e := command.StdoutPipe()
	if e != nil {
		input.Close()
		return e
	}
	if e = command.Start(); e != nil {
		input.Close()
		output.Close()
		return e
	}
	s := &Session{Input: input, Output: output, Listener: listener, OnReady: ready}
	e = s.Serve(ctx)
	_ = command.Process.Kill()
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if e != nil {
		return e
	}
	return waitErr
}
