package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	Magic         uint32 = 0x534D5331
	MaxChannelLen        = 64
	fixedBodySize        = 2 + 8 + 8
)

var (
	ErrBadMagic    = errors.New("protocol: bad magic")
	ErrFrameTooBig = errors.New("protocol: frame exceeds limit")
	ErrBadChannel  = errors.New("protocol: invalid channel")
)

type Frame struct {
	Channel   string
	Sequence  uint64
	Timestamp int64
	Payload   []byte
}

func ReadFrame(r io.Reader, maxFrameBytes uint32) (Frame, error) {
	var prefix [8]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return Frame{}, err
	}
	if binary.BigEndian.Uint32(prefix[0:4]) != Magic {
		return Frame{}, ErrBadMagic
	}
	bodyLen := binary.BigEndian.Uint32(prefix[4:8])
	if bodyLen > maxFrameBytes || uint64(bodyLen)+8 > uint64(maxFrameBytes) {
		return Frame{}, ErrFrameTooBig
	}
	if bodyLen < fixedBodySize+1 {
		return Frame{}, ErrBadChannel
	}

	body := make([]byte, bodyLen)
	if _, err := io.ReadFull(r, body); err != nil {
		return Frame{}, err
	}
	channelLen := int(binary.BigEndian.Uint16(body[0:2]))
	if channelLen < 1 || channelLen > MaxChannelLen {
		return Frame{}, ErrBadChannel
	}
	if fixedBodySize+channelLen > len(body) {
		return Frame{}, ErrBadChannel
	}
	f := Frame{
		Channel:   string(body[fixedBodySize : fixedBodySize+channelLen]),
		Sequence:  binary.BigEndian.Uint64(body[2:10]),
		Timestamp: int64(binary.BigEndian.Uint64(body[10:18])),
	}
	payload := body[fixedBodySize+channelLen:]
	if len(payload) > 0 {
		f.Payload = make([]byte, len(payload))
		copy(f.Payload, payload)
	}
	return f, nil
}

func EncodeFrame(f Frame) ([]byte, error) {
	if len(f.Channel) < 1 || len(f.Channel) > MaxChannelLen {
		return nil, ErrBadChannel
	}
	bodyLen := fixedBodySize + len(f.Channel) + len(f.Payload)
	total := 8 + bodyLen
	buf := make([]byte, total)
	binary.BigEndian.PutUint32(buf[0:4], Magic)
	binary.BigEndian.PutUint32(buf[4:8], uint32(bodyLen))
	binary.BigEndian.PutUint16(buf[8:10], uint16(len(f.Channel)))
	binary.BigEndian.PutUint64(buf[10:18], f.Sequence)
	binary.BigEndian.PutUint64(buf[18:26], uint64(f.Timestamp))
	copy(buf[26:26+len(f.Channel)], f.Channel)
	copy(buf[26+len(f.Channel):], f.Payload)
	return buf, nil
}

func WriteFrame(w io.Writer, f Frame) error {
	buf, err := EncodeFrame(f)
	if err != nil {
		return err
	}
	for len(buf) > 0 {
		n, err := w.Write(buf)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		buf = buf[n:]
	}
	return nil
}

func (f Frame) String() string {
	return fmt.Sprintf("frame{channel=%q seq=%d ts=%d bytes=%d}", f.Channel, f.Sequence, f.Timestamp, len(f.Payload))
}
