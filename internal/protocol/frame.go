package protocol

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	TypeControl byte = 0x01
	TypeOutput  byte = 0x02
)

const headerLen = 9

var ErrTruncated = errors.New("protocol: truncated frame")

type Frame struct {
	Type      byte
	SessionID uint32
	Payload   []byte
}

func WriteFrame(w io.Writer, f Frame) error {
	hdr := make([]byte, headerLen)
	hdr[0] = f.Type
	binary.BigEndian.PutUint32(hdr[1:5], f.SessionID)
	binary.BigEndian.PutUint32(hdr[5:9], uint32(len(f.Payload)))
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if len(f.Payload) > 0 {
		if _, err := w.Write(f.Payload); err != nil {
			return err
		}
	}
	return nil
}

func ReadFrame(r io.Reader) (Frame, error) {
	hdr := make([]byte, headerLen)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return Frame{}, err
	}
	f := Frame{Type: hdr[0], SessionID: binary.BigEndian.Uint32(hdr[1:5])}
	n := binary.BigEndian.Uint32(hdr[5:9])
	if n > 0 {
		f.Payload = make([]byte, n)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return Frame{}, ErrTruncated
		}
	}
	return f, nil
}
