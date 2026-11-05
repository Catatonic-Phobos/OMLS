package renderstream

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

type fragmented struct{ r io.Reader }

func (f fragmented) Read(b []byte) (int, error) {
	if len(b) > 1 {
		b = b[:1]
	}
	return f.r.Read(b)
}
func TestFrameFragmentationAndBounds(t *testing.T) {
	want := Frame{Kind: Data, Channel: 42, Payload: []byte{0, 1, 2, 3, 255}}
	var encoded bytes.Buffer
	if e := Write(&encoded, want); e != nil {
		t.Fatal(e)
	}
	wire := append([]byte(nil), encoded.Bytes()...)
	got, e := Read(fragmented{bytes.NewReader(wire)})
	if e != nil || got.Kind != want.Kind || got.Channel != 42 || !bytes.Equal(got.Payload, want.Payload) {
		t.Fatal(got, e)
	}
	binary.LittleEndian.PutUint32(wire[12:], MaxPayload+1)
	if _, e := Read(bytes.NewReader(wire)); e == nil {
		t.Fatal("oversized payload accepted")
	}
	wire[4] = 2
	if _, e := Read(bytes.NewReader(wire)); e == nil {
		t.Fatal("incompatible protocol accepted")
	}
	if _, e := Read(bytes.NewReader(wire[:5])); e == nil {
		t.Fatal("partial header accepted")
	}
	if e := Write(io.Discard, Frame{Kind: Data, Payload: make([]byte, MaxPayload+1)}); e == nil {
		t.Fatal("oversized output accepted")
	}
}
