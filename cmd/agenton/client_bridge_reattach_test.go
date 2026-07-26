package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestClientBridgeReattach verifies the simulated-remote detach/reattach path:
// produce output over one bridge, detach (close stdin, session survives), then
// open a second bridge to the same session and confirm scrollback replays.
func TestClientBridgeReattach(t *testing.T) {
	bin := buildBinary(t)
	home := shortHome(t)
	writeStubConfig(t, home)
	startDaemonProc(t, bin, home)

	// bridge 1: new + attach + text + accept
	bridge1 := exec.Command(bin, "client")
	bridge1.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
	bridge1.Stderr = os.Stderr
	in1, _ := bridge1.StdinPipe()
	out1, _ := bridge1.StdoutPipe()
	if err := bridge1.Start(); err != nil {
		t.Fatal(err)
	}

	writeFrame(in1, 0x01, 0, []byte(`{"type":"new_session","preset":"stub"}`))
	ft, _, body := readFrame(out1)
	if ft != 0x01 || !bytes.Contains(body, []byte("session_state")) {
		t.Fatalf("new: type=%d body=%q", ft, body)
	}
	sid := parseSessionID(body)
	if sid == 0 {
		t.Fatalf("no sid in %q", body)
	}

	writeFrame(in1, 0x01, sid, []byte(`{"type":"attach","session_id":`+itoa(sid)+`}`))
	time.Sleep(300 * time.Millisecond)
	writeFrame(in1, 0x01, sid, []byte(`{"type":"text_input","session_id":`+itoa(sid)+`,"text":"bridge-persist"}`))
	writeFrame(in1, 0x01, sid, []byte(`{"type":"action","session_id":`+itoa(sid)+`,"action":"accept"}`))

	var got []byte
	end := time.Now().Add(4 * time.Second)
	for !bytes.Contains(got, []byte("bridge-persist")) {
		if time.Now().After(end) {
			t.Fatalf("timeout waiting for output, got: %q", got)
		}
		ft, _, b := readFrameTimeout(out1, 500*time.Millisecond)
		if ft == 0x02 {
			got = append(got, b...)
		}
	}

	// detach: tear down bridge1 (simulates an SSH channel close). Killing the
	// process closes its socket fd, so the daemon's streamTo returns and
	// unsubscribes. The session survives in the daemon.
	_ = in1.Close()
	_ = bridge1.Process.Kill()
	_, _ = bridge1.Process.Wait()
	time.Sleep(300 * time.Millisecond)

	// bridge 2: attach to the same session; scrollback must replay prior output.
	bridge2 := exec.Command(bin, "client")
	bridge2.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
	bridge2.Stderr = os.Stderr
	in2, _ := bridge2.StdinPipe()
	out2, _ := bridge2.StdoutPipe()
	if err := bridge2.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = in2.Close()
		_ = bridge2.Process.Kill()
		_, _ = bridge2.Process.Wait()
	}()

	writeFrame(in2, 0x01, sid, []byte(`{"type":"attach","session_id":`+itoa(sid)+`}`))
	var got2 []byte
	end2 := time.Now().Add(4 * time.Second)
	for !bytes.Contains(got2, []byte("bridge-persist")) {
		if time.Now().After(end2) {
			t.Fatalf("reattach timeout, scrollback got: %q", got2)
		}
		ft, _, b := readFrameTimeout(out2, 500*time.Millisecond)
		if ft == 0x02 {
			got2 = append(got2, b...)
		}
	}

	// kill over bridge2
	writeFrame(in2, 0x01, sid, []byte(`{"type":"kill_session","session_id":`+itoa(sid)+`}`))
}

// keep filepath referenced for future config plumbing in this file
var _ = filepath.Join
