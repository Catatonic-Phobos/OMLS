package renderstream

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestConcurrentChannelsAndCancellation(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	requests, send := io.Pipe()
	receive, replies := io.Pipe()
	peerDone := make(chan struct{})
	go func() {
		defer close(peerDone)
		defer requests.Close()
		defer replies.Close()
		if Write(replies, Frame{Kind: Ready, Payload: []byte(`{"backend_initialized":true}`)}) != nil {
			return
		}
		for {
			f, e := Read(requests)
			if e != nil {
				return
			}
			var response Frame
			switch f.Kind {
			case Open:
				response = Frame{Kind: Opened, Channel: f.Channel}
			case Data:
				response = Frame{Kind: Reply, Channel: f.Channel, Payload: f.Payload}
			case Close:
				response = Frame{Kind: Closed, Channel: f.Channel}
			case Ping:
				response = Frame{Kind: Pong}
			default:
				return
			}
			if Write(replies, response) != nil {
				return
			}
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	done := make(chan error, 1)
	session := Session{Input: send, Output: receive, Listener: listener, OnReady: func(b []byte) error { close(ready); return nil }}
	go func() { done <- session.Serve(ctx) }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("handshake timed out")
	}
	var clients sync.WaitGroup
	for id := 0; id < 4; id++ {
		clients.Add(1)
		go func(id int) {
			defer clients.Done()
			c, e := net.Dial("tcp", listener.Addr().String())
			if e != nil {
				t.Error(e)
				return
			}
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(5 * time.Second))
			data := bytes.Repeat([]byte{byte(id), 255}, 32<<10)
			for n := 0; n < 3; n++ {
				if e := writeAll(c, data); e != nil {
					t.Error(e)
					return
				}
				got := make([]byte, len(data))
				if _, e := io.ReadFull(c, got); e != nil {
					t.Error(e)
					return
				}
				if !bytes.Equal(data, got) {
					t.Error("channels crossed or bytes corrupted")
					return
				}
			}
		}(id)
	}
	clients.Wait()
	cancel()
	select {
	case e := <-done:
		if e != context.Canceled {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session resources survived cancellation")
	}
	select {
	case <-peerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("peer transport did not close")
	}
}

func TestRejectsIncorrectHandshakeAndClosesTransport(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	requests, send := io.Pipe()
	receive, replies := io.Pipe()
	defer requests.Close()
	go func() { defer replies.Close(); _ = Write(replies, Frame{Kind: Opened, Channel: 1}) }()
	session := Session{Input: send, Output: receive, Listener: listener}
	if e := session.Serve(context.Background()); e == nil {
		t.Fatal("accepted data before session handshake")
	}
	if _, e := net.DialTimeout("tcp", listener.Addr().String(), time.Second); e == nil {
		t.Fatal("rejected session left listener open")
	}
}
