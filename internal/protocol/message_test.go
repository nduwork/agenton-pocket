package protocol

import "testing"

func TestEncodeDecodeEnvelope(t *testing.T) {
	out, err := EncodeEnvelope(Envelope{Type: MsgListSessions})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeEnvelope(out)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Type != MsgListSessions {
		t.Fatalf("got %q want %q", got.Type, MsgListSessions)
	}
}

func TestEncodeDecodeEnvelopeWithFields(t *testing.T) {
	in := Envelope{Type: MsgAction, SessionID: 42, Action: "accept", Text: "hi"}
	b, err := EncodeEnvelope(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeEnvelope(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != 42 || got.Action != "accept" || got.Text != "hi" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}
