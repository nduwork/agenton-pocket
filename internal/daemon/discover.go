package daemon

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/nduwork/agenton-pocket/internal/protocol"
)

// agentCmdRe matches command lines of agent CLIs worth surfacing as
// suggestions. ponytail: name-based match — a wrapper script with a different
// name won't be found; extend the alternation when a new agent matters.
var agentCmdRe = regexp.MustCompile(`^(?:\S*/)?(claude|codex|opencode|cortext)(?:\s|$)|^(?:\S*/)?ollama\s+run\s`)

type proc struct {
	pid int
	cmd string
}

// procTree is a snapshot of the host process table: each pid's command and its
// children. Built once per session list so the session title can reflect the
// agent running inside a shell session, and unmanaged discovery can skip agents
// the daemon already owns.
type procTree struct {
	cmd      map[int]string
	children map[int][]int
}

// scanProcs snapshots the process table via `ps -axo pid=,ppid=,command=`.
func scanProcs() procTree {
	out, _ := exec.Command("ps", "-axo", "pid=,ppid=,command=").Output()
	return parseProcTree(string(out))
}

// parseProcTree parses `pid ppid command...` lines (command may contain spaces).
func parseProcTree(psOut string) procTree {
	t := procTree{cmd: map[int]string{}, children: map[int][]int{}}
	for _, line := range strings.Split(psOut, "\n") {
		line = strings.TrimSpace(line)
		i := strings.IndexByte(line, ' ')
		if i < 0 {
			continue
		}
		pid, err := strconv.Atoi(line[:i])
		if err != nil {
			continue
		}
		rest := strings.TrimSpace(line[i+1:])
		j := strings.IndexByte(rest, ' ')
		if j < 0 {
			continue
		}
		ppid, err := strconv.Atoi(rest[:j])
		if err != nil {
			continue
		}
		t.cmd[pid] = strings.TrimSpace(rest[j+1:])
		t.children[ppid] = append(t.children[ppid], pid)
	}
	return t
}

// descendants returns every pid under root (BFS, shallowest first).
func (t procTree) descendants(root int) []int {
	var out []int
	seen := map[int]bool{root: true}
	queue := append([]int(nil), t.children[root]...)
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		out = append(out, pid)
		queue = append(queue, t.children[pid]...)
	}
	return out
}

// agentUnder returns the friendly name of an agent CLI running under root
// (a session's shell), or "" if none — the shallowest match wins, so `claude`
// launched at the prompt beats a nested helper.
func (t procTree) agentUnder(root int) string {
	for _, pid := range t.descendants(root) {
		if agentCmdRe.MatchString(t.cmd[pid]) {
			return firstWordBase(t.cmd[pid])
		}
	}
	return ""
}

// cwdForPids returns each pid's working directory via lsof (macOS has no
// /proc). Best-effort: pids lsof can't inspect are simply absent.
func cwdForPids(pids []int) map[int]string {
	if len(pids) == 0 {
		return nil
	}
	strs := make([]string, len(pids))
	for i, p := range pids {
		strs[i] = strconv.Itoa(p)
	}
	out, _ := exec.Command("lsof", "-a", "-p", strings.Join(strs, ","), "-d", "cwd", "-Fpn").Output()
	cwds := map[int]string{}
	pid := 0
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'n':
			if pid != 0 {
				cwds[pid] = line[1:]
			}
		}
	}
	return cwds
}

// unmanagedSessions discovers agent processes on this host that the daemon
// does not own. Discovery only: they carry command line + cwd so clients can
// clone them into managed sessions; they cannot be attached. Agents running
// *inside* a daemon shell session are excluded — they belong to that session
// (and surface as its title), not as a separate clonable process.
func (d *Daemon) unmanagedSessions(tree procTree) []protocol.SessionInfo {
	owned := map[int]bool{}
	d.mu.Lock()
	for _, s := range d.sessions {
		if p := s.pid(); p != 0 {
			owned[p] = true
			for _, c := range tree.descendants(p) {
				owned[c] = true
			}
		}
	}
	d.mu.Unlock()

	var procs []proc
	for pid, cmd := range tree.cmd {
		if !owned[pid] && agentCmdRe.MatchString(cmd) {
			procs = append(procs, proc{pid: pid, cmd: cmd})
		}
	}
	pids := make([]int, len(procs))
	for i, p := range procs {
		pids[i] = p.pid
	}
	cwds := cwdForPids(pids)

	out := make([]protocol.SessionInfo, 0, len(procs))
	for _, p := range procs {
		out = append(out, protocol.SessionInfo{
			Agent:       firstWordBase(p.cmd),
			Status:      "unmanaged",
			Cwd:         cwds[p.pid],
			Repo:        repoName(cwds[p.pid]),
			CommandLine: p.cmd,
		})
	}
	return out
}

// firstWordBase returns the basename of the command's first word.
func firstWordBase(cmd string) string {
	first := cmd
	if i := strings.IndexByte(cmd, ' '); i >= 0 {
		first = cmd[:i]
	}
	if i := strings.LastIndexByte(first, '/'); i >= 0 {
		first = first[i+1:]
	}
	return first
}
