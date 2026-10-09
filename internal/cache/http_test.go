package cache

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"
)

func TestPlaceRoundTrip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(ctx, addr, NewStore())
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := FetchStat(context.Background(), addr); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cache server did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	payload := bytes.Repeat([]byte("omls"), 3000)
	st, err := Place(context.Background(), addr, 16384, Slice{
		Kind:   KindMemory,
		Source: "unit",
		Data:   payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Capacity != 16384 || st.Used != len(payload) || st.Kind != KindMemory || st.Source != "unit" {
		t.Fatalf("stat %+v", st)
	}
	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
}
