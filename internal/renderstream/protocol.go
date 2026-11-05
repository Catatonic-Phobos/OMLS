// Package renderstream carries bounded Gfxstream channels in one GPU session.
package renderstream

import (
	"encoding/binary"
	"fmt"
	"io"
)

const Version = 1
const MaxPayload = 1 << 20
const MaxChannels = 64
const (
	Open uint16 = iota + 1
	Data
	Close
	Reply
	Closed
	Failure
	Opened
	Ping
	Pong
	Ready
)

type Frame struct {
	Kind    uint16
	Channel uint32
	Payload []byte
}

func Read(r io.Reader) (Frame, error) {
	var h [16]byte
	if _, e := io.ReadFull(r, h[:]); e != nil {
		return Frame{}, e
	}
	if string(h[:4]) != "OMGR" || binary.LittleEndian.Uint16(h[4:]) != Version {
		return Frame{}, fmt.Errorf("incompatible graphics session protocol")
	}
	kind := binary.LittleEndian.Uint16(h[6:])
	n := binary.LittleEndian.Uint32(h[12:])
	if kind < Open || kind > Ready || n > MaxPayload {
		return Frame{}, fmt.Errorf("invalid graphics frame")
	}
	f := Frame{Kind: kind, Channel: binary.LittleEndian.Uint32(h[8:]), Payload: make([]byte, n)}
	_, e := io.ReadFull(r, f.Payload)
	return f, e
}
func Write(w io.Writer, f Frame) error {
	if len(f.Payload) > MaxPayload || f.Kind < Open || f.Kind > Ready {
		return fmt.Errorf("invalid graphics frame")
	}
	var h [16]byte
	copy(h[:4], "OMGR")
	binary.LittleEndian.PutUint16(h[4:], Version)
	binary.LittleEndian.PutUint16(h[6:], f.Kind)
	binary.LittleEndian.PutUint32(h[8:], f.Channel)
	binary.LittleEndian.PutUint32(h[12:], uint32(len(f.Payload)))
	if e := writeAll(w, h[:]); e != nil {
		return e
	}
	return writeAll(w, f.Payload)
}
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, e := w.Write(b)
		if e != nil {
			return e
		}
		if n <= 0 || n > len(b) {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
