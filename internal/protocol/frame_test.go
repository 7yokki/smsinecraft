package protocol

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	in := Frame{
		Channel:   ChannelChunkResponse,
		Sequence:  42,
		Timestamp: 1_700_000_000_000,
		Payload:   []byte{0, 1, 2, 3, 255},
	}
	wire, err := EncodeFrame(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := ReadFrame(bytes.NewReader(wire), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if out.Channel != in.Channel || out.Sequence != in.Sequence || out.Timestamp != in.Timestamp {
		t.Fatalf("envelope mismatch: %+v", out)
	}
	if !bytes.Equal(out.Payload, in.Payload) {
		t.Fatalf("payload mismatch: %v", out.Payload)
	}
}

func TestFrameRejectsBadMagic(t *testing.T) {
	wire, err := EncodeFrame(Frame{Channel: ChannelConnect})
	if err != nil {
		t.Fatal(err)
	}
	wire[0] = 0
	if _, err := ReadFrame(bytes.NewReader(wire), 1024); !errors.Is(err, ErrBadMagic) {
		t.Fatalf("expected bad magic, got %v", err)
	}
}

func TestFrameRejectsOversize(t *testing.T) {
	wire, err := EncodeFrame(Frame{Channel: ChannelConnect, Payload: make([]byte, 100)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFrame(bytes.NewReader(wire), 64); !errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("expected frame too big, got %v", err)
	}
}

func TestFrameRejectsInvalidChannel(t *testing.T) {
	if _, err := EncodeFrame(Frame{Channel: ""}); !errors.Is(err, ErrBadChannel) {
		t.Fatalf("expected bad channel, got %v", err)
	}
	wire, _ := EncodeFrame(Frame{Channel: "x"})
	wire[8] = 0
	wire[9] = 0
	if _, err := ReadFrame(bytes.NewReader(wire), 1024); !errors.Is(err, ErrBadChannel) {
		t.Fatalf("expected bad channel, got %v", err)
	}
}

func TestReadFrameTruncated(t *testing.T) {
	wire, err := EncodeFrame(Frame{Channel: ChannelPosition, Payload: []byte("abc")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ReadFrame(bytes.NewReader(wire[:len(wire)-1]), 1024)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected unexpected EOF, got %v", err)
	}
}
