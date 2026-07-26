package protocol

import (
	"bytes"
	"testing"
)

func TestWriteThenReadFrame(t *testing.T) {
	var buf bytes.Buffer
	want := Frame{Type: TypeControl, SessionID: 7, Payload: []byte(`{"type":"ping"}`)}
	if err := WriteFrame(&buf, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Type != want.Type || got.SessionID != want.SessionID || !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestReadFrameRejectsTruncated(t *testing.T) {
	buf := bytes.NewReader([]byte{TypeOutput, 0, 0, 0, 7, 0, 0, 0, 5, 'a'}) // len 5, only 1 byte
	if _, err := ReadFrame(buf); err == nil {
		t.Fatal("expected error on truncated payload")
	}
}

func TestZeroPayloadFrame(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, Frame{Type: TypeControl, SessionID: 0, Payload: nil}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != 0 || len(got.Payload) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestEnvelopeActiveRoundTrip(t *testing.T) {
	for _, want := range []*bool{nil, boolp(true), boolp(false)} {
		e := Envelope{Type: MsgSessionState, SessionID: 7, Active: want}
		b, err := EncodeEnvelope(e)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		got, err := DecodeEnvelope(b)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if (got.Active == nil) != (want == nil) ||
			(want != nil && *got.Active != *want) {
			t.Fatalf("Active round-trip: got %v want %v", got.Active, want)
		}
	}
}

func boolp(b bool) *bool { return &b }
