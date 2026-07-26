#!/usr/bin/env python3
# PTY-driven smoke test for the agenton TUI. Exercises the new command-input
# entry flow and the headless-terminal session view by launching the ANSI
# demo stub and interacting with it through shortcut + input modes.
import os, pty, sys, time, select, errno, subprocess

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
STUB = os.path.join(REPO, "internal", "daemon", "testdata", "ansidemo.go")

def feed(fd, data, delay=0.15):
    time.sleep(delay)
    os.write(fd, data)

def read_avail(fd, timeout=1.0, maxb=65536):
    out = b""
    end = time.time() + timeout
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.05)
        if r:
            try:
                chunk = os.read(fd, maxb)
            except OSError as e:
                if e.errno == errno.EIO:
                    break
                raise
            if not chunk:
                break
            out += chunk
            # Answer terminal queries (cursor position / background color) so
            # the TUI doesn't sit on its startup probe before first paint.
            if b"\x1b[6n" in chunk:
                os.write(fd, b"\x1b[1;1R")
            if b"\x1b]11;?" in chunk:
                os.write(fd, b"\x1b]11;rgb:0000/0000/0000\x1b\\")
    return out

def main():
    # Build a fresh binary so the smoke test runs the current source. Build
    # into the repo dir (/tmp may be mounted noexec on some sandboxes).
    binpath = os.path.join(REPO, "agenton-smoke")
    subprocess.check_call(["go", "build", "-o", binpath, "./cmd/agenton"], cwd=REPO)

    pid, fd = pty.fork()
    if pid == 0:
        env = dict(os.environ)
        env["HOME"] = "/tmp/agenton-smoke-home"
        os.makedirs("/tmp/agenton-smoke-home", exist_ok=True)
        sock = os.path.join(os.path.expanduser("~"), ".agenton", "agenton.sock")
        os.execvpe(binpath, ["agenton", "tui", "-socket", sock], env)
        os._exit(127)

    time.sleep(0.6)
    screen = read_avail(fd, 1.0)
    sys.stdout.write("=== initial entry screen ===\n")
    sys.stdout.write(screen.decode(errors="replace"))

    sys.stdout.write("\n=== 'n' opens command input ===\n")
    feed(fd, b"n")
    sys.stdout.write(read_avail(fd, 0.8).decode(errors="replace"))

    sys.stdout.write("\n=== type the demo command, watch hints ===\n")
    feed(fd, b"go", 0.2)
    sys.stdout.write(read_avail(fd, 0.5).decode(errors="replace"))
    feed(fd, b" run " + STUB.encode(), 0.2)
    sys.stdout.write(read_avail(fd, 0.5).decode(errors="replace"))

    sys.stdout.write("\n=== enter launches + attaches ===\n")
    feed(fd, b"\r")
    sys.stdout.write(read_avail(fd, 1.2).decode(errors="replace"))

    sys.stdout.write("\n=== '8' toggles input mode ===\n")
    feed(fd, b"8", 0.2)
    sys.stdout.write(read_avail(fd, 0.5).decode(errors="replace"))

    sys.stdout.write("\n=== type 'hello world', enter sends to PTY ===\n")
    feed(fd, b"hello world", 0.2)
    sys.stdout.write(read_avail(fd, 0.5).decode(errors="replace"))
    feed(fd, b"\r", 0.2)
    sys.stdout.write(read_avail(fd, 1.2).decode(errors="replace"))

    sys.stdout.write("\n=== '1' (accept) ===\n")
    feed(fd, b"1", 0.2)
    sys.stdout.write(read_avail(fd, 0.8).decode(errors="replace"))

    sys.stdout.write("\n=== ctrl+d (detach) ===\n")
    feed(fd, b"\x04", 0.2)
    sys.stdout.write(read_avail(fd, 1.2).decode(errors="replace"))

    sys.stdout.write("\n=== 'q' (quit) ===\n")
    feed(fd, b"q", 0.2)
    try:
        sys.stdout.write(read_avail(fd, 1.0).decode(errors="replace"))
    except OSError:
        pass
    try:
        os.close(fd)
    except OSError:
        pass
    os.waitpid(pid, 0)

if __name__ == "__main__":
    main()