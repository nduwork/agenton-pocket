#!/usr/bin/env python3
# Asserting PTY-driven e2e for the TUI. Requires a running `agenton daemon`
# on the default socket. Two phases:
#   1. interactive: attach bash, send input twice (regression: terminal must
#      keep updating), shortcut key, detach (notice), reattach (scrollback
#      replay), kill with confirmation.
#   2. resize: a session attached mid-run must get a PTY sized to the window,
#      and follow live window resizes (regression: stuck at 80x24 default).
import os, pty, time, select, re, fcntl, termios, struct, signal, subprocess

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))


def setwinsz(fd, rows, cols):
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))


def read_avail(fd, timeout=1.0):
    out = b""
    end = time.time() + timeout
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.05)
        if r:
            try:
                c = os.read(fd, 65536)
            except OSError:
                break
            if not c:
                break
            out += c
            # Answer terminal queries so the TUI doesn't block on its probe.
            if b"\x1b[6n" in c:
                os.write(fd, b"\x1b[1;1R")
            if b"\x1b]11;?" in c:
                os.write(fd, b"\x1b]11;rgb:0000/0000/0000\x1b\\")
    return out


def plain(b):
    s = b.decode(errors="replace")
    s = re.sub(r"\x1b\][^\x07\x1b]*(\x07|\x1b\\)", "", s)
    s = re.sub(r"\x1b\[[0-9;?]*[A-Za-z]", "", s)
    return s


def spawn(binpath, rows=24, cols=90):
    pid, fd = pty.fork()
    if pid == 0:
        os.execvp(binpath, ["agenton", "tui"])
        os._exit(127)
    setwinsz(fd, rows, cols)
    os.kill(pid, signal.SIGWINCH)
    return pid, fd


def finish(pid, fd):
    try:
        os.write(fd, b"q")
    except OSError:
        pass
    time.sleep(0.4)
    try:
        os.close(fd)
    except OSError:
        pass
    os.waitpid(pid, 0)


def phase_interactive(binpath):
    pid, fd = spawn(binpath)
    s = plain(read_avail(fd, 2.0))
    assert "agenton" in s, "no entry screen: %r" % s[:200]
    print("OK entry screen")

    os.write(fd, b"n"); read_avail(fd, 0.5)
    os.write(fd, b"bash"); read_avail(fd, 0.4)
    os.write(fd, b"\r")
    s = plain(read_avail(fd, 2.0))
    assert "SHORTCUT" in s, "no session view: %r" % s[:400]
    print("OK attached, session view painted")

    for i, (expr, want) in enumerate([("echo tui_$((40+2))_live", "tui_42_live"),
                                      ("echo again_$((50+5))", "again_55")]):
        os.write(fd, b"8"); read_avail(fd, 0.4)
        os.write(fd, expr.encode()); read_avail(fd, 0.4)
        os.write(fd, b"\r")
        s = plain(read_avail(fd, 3.0))
        assert want in s, "terminal stalled on input %d: %r" % (i + 1, s[-600:])
    print("OK terminal keeps updating after input")

    os.write(fd, b"1"); read_avail(fd, 1.0)  # shortcut: accept (Enter)

    os.write(fd, b"\x04")
    s = plain(read_avail(fd, 2.0))
    assert "detached" in s and "bash" in s, "no entry after detach: %r" % s[-400:]
    print("OK detach shows notice; session still listed")

    os.write(fd, b"\r")
    s = plain(read_avail(fd, 2.5))
    assert "tui_42_live" in s, "scrollback replay missing: %r" % s[-600:]
    print("OK reattach replays scrollback")

    os.write(fd, b"\x04"); read_avail(fd, 1.0)
    os.write(fd, b"d")
    s = plain(read_avail(fd, 1.0))
    assert "kill" in s and "confirm" in s, "no kill confirmation: %r" % s[-300:]
    os.write(fd, b"d")
    s = plain(read_avail(fd, 1.5))
    assert "no sessions" in s, "session not gone after kill: %r" % s[-300:]
    print("OK kill requires confirmation and removes session")
    finish(pid, fd)


def phase_resize(binpath):
    pid, fd = spawn(binpath, rows=30, cols=100)
    read_avail(fd, 2.0)
    os.write(fd, b"n"); read_avail(fd, 0.5)
    os.write(fd, b"bash"); read_avail(fd, 0.4)
    os.write(fd, b"\r"); read_avail(fd, 2.0)

    os.write(fd, b"8"); read_avail(fd, 0.4)
    os.write(fd, b"echo cols_$(tput cols)"); read_avail(fd, 0.4)
    os.write(fd, b"\r")
    s = plain(read_avail(fd, 3.0))
    m = re.search(r"cols_(\d+)", s)
    assert m and m.group(1) == "100", "PTY not sized to window: %s" % (m.group(1) if m else s[-300:])
    print("OK mid-run attach gets window-sized PTY (100 cols)")

    setwinsz(fd, 30, 120)
    os.kill(pid, signal.SIGWINCH)
    read_avail(fd, 1.0)
    os.write(fd, b"8"); read_avail(fd, 0.4)
    os.write(fd, b"echo cols2_$(tput cols)"); read_avail(fd, 0.4)
    os.write(fd, b"\r")
    s = plain(read_avail(fd, 3.0))
    m = re.search(r"cols2_(\d+)", s)
    assert m and m.group(1) == "120", "PTY did not follow resize: %s" % (m.group(1) if m else s[-300:])
    print("OK live window resize reflows the session (120 cols)")

    os.write(fd, b"\x04"); read_avail(fd, 1.0)
    os.write(fd, b"d"); read_avail(fd, 0.4)
    os.write(fd, b"d"); read_avail(fd, 1.0)
    finish(pid, fd)


def main():
    binpath = os.path.join(REPO, "agenton-smoke")
    subprocess.check_call(["go", "build", "-o", binpath, "./cmd/agenton"], cwd=REPO)
    phase_interactive(binpath)
    phase_resize(binpath)
    print("ALL TUI E2E CHECKS PASSED")


if __name__ == "__main__":
    main()
