package daemon

import (
	"bytes"
	"net"
	"strings"
	"testing"

	"github.com/nduwork/agenton-pocket/internal/protocol"
)

func TestSplitFrames(t *testing.T) {
	cases := []struct {
		name string
		n    int
		max  int
		want int // expected chunk count
	}{
		{"empty", 0, 64, 0},
		{"under", 10, 64, 1},
		{"exact", 64, 64, 1},
		{"over", 65, 64, 2},
		{"multiple", 200, 64, 4}, // 64+64+64+8
		{"nonpositive-max", 100, 0, 1}, // guard: no split rather than infinite loop
	}
	for _, c := range cases {
		b := bytes.Repeat([]byte("x"), c.n)
		chunks := splitFrames(b, c.max)
		if len(chunks) != c.want {
			t.Errorf("%s: got %d chunks, want %d", c.name, len(chunks), c.want)
		}
		for i, ch := range chunks {
			if c.max > 0 && len(ch) > c.max {
				t.Errorf("%s: chunk %d is %d bytes, exceeds max %d", c.name, i, len(ch), c.max)
			}
		}
		if got := bytes.Join(chunks, nil); !bytes.Equal(got, b) {
			t.Errorf("%s: reassembly lost data", c.name)
		}
	}
}

// TestStreamToChunksReplay reproduces the phone "disconnected" bug end-to-end: a
// long session's attach replay must reach the client as multiple frames each
// within maxFrameBytes, not one oversized frame. Driving streamTo itself (over a
// real pipe) is what makes this catch a revert of the split loop — a helper-only
// test would still pass if streamTo went back to a single cw.write.
func TestStreamToChunksReplay(t *testing.T) {
	s := NewSession(1, "t", Preset{Command: "claude"})
	// Fill scrollback well past a single 64 KiB frame.
	for i := 0; i < 4000; i++ {
		s.handleOutput([]byte(strings.Repeat("A", 60) + "\r\n"))
	}
	// Reference replay bytes — serializeReplay is a pure read of emu+modes, so it
	// matches what streamTo's own subscribe will serialize (no output in between).
	s.mu.Lock()
	want := bytes.Join(serializeReplay(s.emu, s.modes), nil)
	s.mu.Unlock()
	if len(want) <= maxFrameBytes {
		t.Fatalf("test premise stale: replay is %d bytes, not over the %d limit", len(want), maxFrameBytes)
	}

	cli, srv := net.Pipe()
	go (&Daemon{}).streamTo(newConnWriter(srv), s) // streamTo touches only s and cw, not d

	var got []byte
	frames := 0
	for len(got) < len(want) {
		f, err := protocol.ReadFrame(cli)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if f.Type != protocol.TypeOutput {
			continue
		}
		if len(f.Payload) > maxFrameBytes {
			t.Fatalf("frame %d is %d bytes, exceeds limit %d (streamTo did not chunk)", frames, len(f.Payload), maxFrameBytes)
		}
		got = append(got, f.Payload...)
		frames++
	}
	if frames < 2 {
		t.Fatalf("oversized replay should span multiple frames, got %d", frames)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("reassembled stream does not equal the replay")
	}

	// Unblock streamTo (now ranging the live channel) the way a session exit does:
	// close and drop the subscription, then close the pipe.
	s.mu.Lock()
	for ch := range s.subs {
		close(ch)
		delete(s.subs, ch)
	}
	s.mu.Unlock()
	_ = cli.Close()
}
