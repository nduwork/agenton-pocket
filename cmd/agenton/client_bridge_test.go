package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "agenton")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// shortHome returns a short temp HOME so the default socket path
// (~/.agenton/agenton.sock) fits macOS' 104-char Unix socket path limit.
func shortHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "agenton-home")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	return home
}

func writeStubConfig(t *testing.T, home string) {
	t.Helper()
	stub, err := filepath.Abs(filepath.Join("..", "..", "internal", "daemon", "testdata", "echo.go"))
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "agenton")
	os.MkdirAll(cfgDir, 0o700)
	body := "[preset.stub]\nagent=\"stub\"\ncommand=\"go\"\nargs=[\"run\",\"" + stub + "\"]\n"
	os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(body), 0o600)
}

func startDaemonProc(t *testing.T, bin, home string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(bin, "daemon")
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	sock := filepath.Join(home, ".agenton", "agenton.sock")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); err == nil {
			return cmd
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("daemon socket never appeared")
	return nil
}

func writeFrame(w io.Writer, ftype byte, sid uint32, payload []byte) {
	hdr := make([]byte, 9)
	hdr[0] = ftype
	binary.BigEndian.PutUint32(hdr[1:5], sid)
	binary.BigEndian.PutUint32(hdr[5:9], uint32(len(payload)))
	w.Write(append(hdr, payload...))
}

func readFrame(r io.Reader) (byte, uint32, []byte) {
	hdr := make([]byte, 9)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return 0, 0, nil
	}
	sid := binary.BigEndian.Uint32(hdr[1:5])
	n := binary.BigEndian.Uint32(hdr[5:9])
	body := make([]byte, n)
	if n > 0 {
		io.ReadFull(r, body)
	}
	return hdr[0], sid, body
}

func readFrameTimeout(r io.Reader, d time.Duration) (byte, uint32, []byte) {
	type res struct {
		t byte
		s uint32
		b []byte
	}
	ch := make(chan res, 1)
	go func() {
		t, s, b := readFrame(r)
		ch <- res{t, s, b}
	}()
	select {
	case v := <-ch:
		return v.t, v.s, v.b
	case <-time.After(d):
		return 0, 0, nil
	}
}

func TestClientBridgeListAndNew(t *testing.T) {
	bin := buildBinary(t)
	home := shortHome(t)
	writeStubConfig(t, home)
	startDaemonProc(t, bin, home)

	bridge := exec.Command(bin, "client")
	bridge.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
	bridge.Stderr = os.Stderr
	stdin, _ := bridge.StdinPipe()
	stdout, _ := bridge.StdoutPipe()
	if err := bridge.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = bridge.Process.Kill()
		_, _ = bridge.Process.Wait()
	})

	// list -> session_list
	writeFrame(stdin, 0x01, 0, []byte(`{"type":"list_sessions"}`))
	ft, _, body := readFrame(stdout)
	if ft != 0x01 || !bytes.Contains(body, []byte("session_list")) {
		t.Fatalf("list: type=%d body=%q", ft, body)
	}

	// new -> session_state (session id is in the JSON body, not the frame header)
	writeFrame(stdin, 0x01, 0, []byte(`{"type":"new_session","preset":"stub"}`))
	ft, _, body = readFrame(stdout)
	if ft != 0x01 || !bytes.Contains(body, []byte("session_state")) {
		t.Fatalf("new: type=%d body=%q", ft, body)
	}
	sid := parseSessionID(body)
	if sid == 0 {
		t.Fatalf("no session id in %q", body)
	}

	// attach + text + accept -> expect echoed output over the bridge
	writeFrame(stdin, 0x01, sid, []byte(`{"type":"attach","session_id":`+itoa(sid)+`}`))
	time.Sleep(300 * time.Millisecond)
	writeFrame(stdin, 0x01, sid, []byte(`{"type":"text_input","session_id":`+itoa(sid)+`,"text":"hello remote"}`))
	writeFrame(stdin, 0x01, sid, []byte(`{"type":"action","session_id":`+itoa(sid)+`,"action":"accept"}`))

	var got []byte
	end := time.Now().Add(4 * time.Second)
	for !bytes.Contains(got, []byte("hello remote")) {
		if time.Now().After(end) {
			t.Fatalf("timeout, got: %q", got)
		}
		ft, _, b := readFrameTimeout(stdout, 500*time.Millisecond)
		if ft == 0 && b == nil {
			continue
		}
		if ft == 0x02 {
			got = append(got, b...)
		}
	}

	// kill
	writeFrame(stdin, 0x01, sid, []byte(`{"type":"kill_session","session_id":`+itoa(sid)+`}`))
}

func parseSessionID(body []byte) uint32 {
	idx := bytes.Index(body, []byte(`"session_id":`))
	if idx < 0 {
		return 0
	}
	rest := body[idx+len(`"session_id":`):]
	var n uint32
	for _, c := range rest {
		if c >= '0' && c <= '9' {
			n = n*10 + uint32(c-'0')
		} else if n > 0 {
			break
		}
	}
	return n
}

func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
