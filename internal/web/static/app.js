import { TYPE_CONTROL, TYPE_OUTPUT, encodeControl, decodeFrame, parseControl } from './frame.js';

const $ = (id) => document.getElementById(id);

// --- toast + banner ----------------------------------------------------------
let toastTimer;
function toast(msg) {
  const el = $('toast');
  el.textContent = msg;
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.hidden = true; }, 4000);
}
function showBanner(show) { $('banner').hidden = !show; }

// --- websocket helper ---------------------------------------------------------
function connect(onFrame, onClose) {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const ws = new WebSocket(`${proto}://${location.host}/ws`);
  ws.binaryType = 'arraybuffer';
  ws.onmessage = (e) => onFrame(decodeFrame(e.data));
  ws.onclose = onClose;
  return ws;
}
function send(ws, msg) {
  if (ws && ws.readyState === WebSocket.OPEN) ws.send(encodeControl(msg));
}

// --- entry screen --------------------------------------------------------------
let entryWS = null;
let pendingName = '';

function entryConnect() {
  showBanner(false);
  if (entryWS) { entryWS.onclose = null; entryWS.close(); }
  entryWS = connect(onEntryFrame, () => showBanner(true));
  entryWS.onopen = () => send(entryWS, { type: 'list_sessions' });
}

function onEntryFrame(f) {
  if (f.type !== TYPE_CONTROL) return;
  const env = parseControl(f.payload);
  switch (env.type) {
    case 'session_list':
      renderSessions(env.sessions || []);
      break;
    case 'session_state':
      // a session we just created: jump straight into it
      if (env.session_id) enterSession(env.session_id, pendingName || `session ${env.session_id}`);
      pendingName = '';
      break;
    case 'dir_list':
      renderDirList(env.path || '', env.dirs || []);
      break;
    case 'error':
      toast(env.message || 'error');
      break;
  }
}

function renderSessions(sessions) {
  const ul = $('session-list');
  ul.replaceChildren();
  if (sessions.length === 0) {
    const li = document.createElement('li');
    li.innerHTML = '<div class="meta"><div class="sub">no sessions — start one below</div></div>';
    ul.appendChild(li);
    return;
  }
  for (const s of sessions.sort((a, b) => a.id - b.id)) {
    const li = document.createElement('li');
    const dot = document.createElement('span');
    dot.className = `dot ${s.status === 'running' ? 'running' : ''}`;
    const meta = document.createElement('div');
    meta.className = 'meta';
    const name = document.createElement('div');
    name.className = 'name';
    // title follows what's running inside the session (daemon reports the agent
    // when one is running in the shell, else the shell itself).
    name.textContent = `${repoLabel(s)} · ${terminalName(s.agent)}`;
    const sub = document.createElement('div');
    sub.className = 'sub';
    sub.textContent = s.status;
    meta.append(name, sub);
    const kill = document.createElement('button');
    kill.className = 'kill';
    kill.textContent = '✕';
    kill.onclick = (e) => {
      e.stopPropagation();
      if (confirm(`Kill session "${repoLabel(s)}"?`)) {
        send(entryWS, { type: 'kill_session', session_id: s.id });
        send(entryWS, { type: 'list_sessions' });
      }
    };
    li.append(dot, meta, kill);
    li.onclick = () => enterSession(s.id, repoLabel(s), s.agent);
    ul.appendChild(li);
  }
}

// Short working-directory label: daemon-provided repo/folder name, with a
// client-side folder-name fallback so it still works for older daemons.
function repoLabel(s) {
  if (s.repo) return s.repo;
  if (s.cwd) return s.cwd.split('/').filter(Boolean).pop() || s.cwd;
  return '—';
}

// Friendly terminal name from an agent path/command — "claude" or
// "/usr/local/bin/claude" → "Claude".
function terminalName(agent) {
  if (!agent) return '—';
  const base = agent.split('/').pop();
  return base.charAt(0).toUpperCase() + base.slice(1);
}

// --- command chips: recent (localStorage) + claude/codex/ollama placeholders --
// `ollama launch` starts claude/codex wired to a local/cloud ollama model —
// unlike `ollama run`, which is just a chat REPL with no agent.
const PLACEHOLDERS = ['claude', 'codex', 'ollama launch claude --model glm-5.2:cloud', 'ollama launch codex --model gemma4:12b-mlx'];
const HISTORY_KEY = 'agenton.cmdHistory';

function cmdHistory() {
  try { return JSON.parse(localStorage.getItem(HISTORY_KEY)) || []; } catch { return []; }
}

function rememberCmd(command, cwd) {
  const hist = cmdHistory().filter((h) => h.command !== command);
  hist.unshift({ command, cwd });
  localStorage.setItem(HISTORY_KEY, JSON.stringify(hist.slice(0, 20)));
}

// recent commands you actually typed (up to 6, styled .recent) then the
// claude/codex/ollama quick-start placeholders — deduped. Live-session command
// lines are deliberately excluded: they carry session-specific args.
function renderChips() {
  const wrap = $('cmd-chips');
  wrap.replaceChildren();
  const seen = new Set();
  const entries = [];
  for (const h of cmdHistory()) {
    if (entries.length >= 6) break;
    if (!h.command || seen.has(h.command)) continue;
    seen.add(h.command);
    entries.push({ command: h.command, cwd: h.cwd, recent: true });
  }
  for (const c of PLACEHOLDERS) {
    if (seen.has(c)) continue;
    seen.add(c);
    entries.push({ command: c, recent: false });
  }
  for (const e of entries) {
    const b = document.createElement('button');
    b.type = 'button';
    const label = document.createElement('span');
    label.textContent = e.command;
    b.appendChild(label);
    if (e.recent) b.className = 'recent';
    b.onclick = () => {
      $('new-command').value = e.command;
      $('new-command').dispatchEvent(new Event('input')); // refresh the submit label
      if (e.cwd) $('new-cwd').value = e.cwd;
      $('new-command').focus();
    };
    wrap.appendChild(b);
    // A label longer than the chip slides back and forth instead of masking
    // its tail with … — the tail (the model name) is the important part.
    const overflow = b.scrollWidth - b.clientWidth;
    if (overflow > 0) {
      b.classList.add('slide');
      b.style.setProperty('--slide', `-${overflow}px`);
    }
  }
}
renderChips();

// --- terminal font size: shrinking the font fits more columns, so the agent
// redraws its bottom status line with more room. Persisted per-device.
const FONT_KEY = 'agenton.termFontSize';
// larger steps first for readability, then the small ones that fit more
// columns — same cycle as the iOS Aa button.
const FONT_STEPS = [13, 15, 17, 11, 9];
let termFontSize = (() => { try { const v = JSON.parse(localStorage.getItem(FONT_KEY)); return FONT_STEPS.includes(v) ? v : 13; } catch { return 13; } })();

function applyFontSize() {
  if (!term) return;
  term.options.fontSize = termFontSize;
  sizeTerminal();
  $('btn-font').classList.toggle('compact', termFontSize < 13);
}
function cycleFontSize() {
  const i = FONT_STEPS.indexOf(termFontSize);
  termFontSize = FONT_STEPS[(i + 1) % FONT_STEPS.length];
  try { localStorage.setItem(FONT_KEY, JSON.stringify(termFontSize)); } catch {}
  applyFontSize();
}
$('btn-font').addEventListener('click', cycleFontSize);

$('new-form').addEventListener('submit', (e) => {
  e.preventDefault();
  const line = $('new-command').value.trim();
  // ponytail: whitespace split — quoted args unsupported in 1.1
  // empty command → a plain login shell on the host (daemon resolves $SHELL)
  const parts = line ? line.split(/\s+/) : [''];
  pendingName = parts[0] || 'shell';
  const cwd = $('new-cwd').value.trim();
  send(entryWS, {
    type: 'new_session_cmd',
    command: parts[0],
    args: parts.slice(1),
    cwd,
    agent_label: parts[0],
  });
  if (line) {
    rememberCmd(line, cwd);
    renderChips();
  }
  $('new-command').value = '';
  $('new-go').textContent = 'New shell';
});
$('new-command').addEventListener('input', () => {
  $('new-go').textContent = $('new-command').value.trim() ? 'New session' : 'New shell';
});

$('refresh').onclick = () => send(entryWS, { type: 'list_sessions' });
$('banner-reconnect').onclick = () => {
  if ($('screen-session').hidden) entryConnect();
  else reattach();
};

// --- directory browser (cwd picker) ------------------------------------------
// The browser can't see the daemon host's disk, so each step asks the daemon
// (list_dir) and renders its reply (dir_list) — mirrors the iOS folder picker.
let dirPath = '';

$('btn-browse').onclick = () => {
  dirPath = '';
  $('dir-path').textContent = '…';
  $('dir-list').replaceChildren();
  send(entryWS, { type: 'list_dir' }); // empty path = home
  $('dirpicker').showModal();
};
$('dir-up').onclick = () => send(entryWS, { type: 'list_dir', path: parentPath(dirPath) });
$('dir-home').onclick = () => send(entryWS, { type: 'list_dir' });
$('dir-cancel').onclick = () => $('dirpicker').close();
$('dir-use').onclick = () => { if (dirPath) $('new-cwd').value = dirPath; $('dirpicker').close(); };

function parentPath(p) {
  const trimmed = p.replace(/\/+$/, '');
  const i = trimmed.lastIndexOf('/');
  return i <= 0 ? '/' : trimmed.slice(0, i);
}
function joinPath(base, name) {
  if (!base) return name;
  return base.replace(/\/+$/, '') + '/' + name;
}
function renderDirList(path, dirs) {
  dirPath = path;
  $('dir-path').textContent = path || '…';
  const list = $('dir-list');
  list.replaceChildren();
  if (!dirs.length) {
    const d = document.createElement('div');
    d.className = 'dir-empty';
    d.textContent = 'no subfolders';
    list.appendChild(d);
    return;
  }
  for (const name of dirs) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'dir-item';
    b.textContent = '📁 ' + name;
    b.onclick = () => send(entryWS, { type: 'list_dir', path: joinPath(path, name) });
    list.appendChild(b);
  }
}

// --- session view ---------------------------------------------------------------
let sessWS = null;
let sessId = 0;
let term = null;
let fit = null;
const custom = { custom_1: '', custom_2: '' }; // rebound values, for labels + dispatch

// Set a custom button's stored value and its visible label; an empty value
// resets to the default "Custom N". Shared by the rebind dialog (optimistic
// update) and the daemon's on-attach restore (set_button push), so a rebind
// survives detaching and re-attaching to the session.
function setCustomLabel(action, value) {
  custom[action] = value || '';
  const btn = action === 'custom_1' ? $('btn-custom1') : $('btn-custom2');
  const dflt = action === 'custom_1' ? 'Custom 1' : 'Custom 2';
  btn.textContent = value ? (value.length > 10 ? value.slice(0, 9) + '…' : value) : dflt;
}

let sessTitlePrefix = ''; // repo label, kept so live agent pushes can rebuild "repo · Agent"

// The header follows the agent actually running in the session: the daemon
// pushes session_state.agent_label as you launch/exit claude inside a shell, and
// this rebuilds the title so it reads "repo · Claude" instead of "repo · Zsh".
function setSessionTitle(agent, fallback) {
  const label = agent ? terminalName(agent) : '';
  if (sessTitlePrefix && label) $('session-title').textContent = `${sessTitlePrefix} · ${label}`;
  else $('session-title').textContent = label || fallback || `session ${sessId}`;
}

function enterSession(id, repoOrName, agent) {
  sessId = id;
  $('screen-entry').hidden = true;
  $('screen-session').hidden = false;
  sessTitlePrefix = agent !== undefined ? repoOrName : ''; // list passes repo+agent; created path passes a plain name
  setSessionTitle(agent, repoOrName);
  if (!term) {
    term = new Terminal({
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
      theme: { background: '#0f1115', foreground: '#d8dee9' },
      disableStdin: true, // output-only: all input goes through buttons/text bar
      scrollback: 5000,
    });
    fit = new FitAddon.FitAddon();
    term.loadAddon(fit);
    term.open($('term'));
  }
  term.reset();
  setController(false); // like iOS, every attach starts in Terminal mode;
  // the daemon parks us right away (active:false) if someone else owns the size
  applyFontSize();
  reattach();
}

// --- Terminal / Controller modes ----------------------------------------------
// One PTY has one size, so only the size owner ("active") renders the terminal.
// Parked clients hide it and become a full-screen remote: pad + text bar (the
// web stand-in for iOS's hold-to-talk). One-directional like iOS: a broadcast
// can only park us — entering Terminal mode is always an explicit tap.
let isController = false;

function setController(on) {
  isController = on;
  $('screen-session').classList.toggle('controller', on);
  $('btn-mode').textContent = on ? 'Term' : 'Pad';
  $('btn-font').hidden = on;
}

$('btn-mode').onclick = () => {
  const on = !isController;
  setController(on);
  send(sessWS, { type: 'set_active', session_id: sessId, active: !on });
  // reclaiming the terminal: refit now that #term is visible again, so the PTY
  // reflows to this screen immediately instead of waiting for a window resize
  if (!on) sizeTerminal();
};

function reattach() {
  showBanner(false);
  if (sessWS) { sessWS.onclose = null; sessWS.close(); }
  sessWS = connect(onSessionFrame, () => showBanner(true));
  sessWS.onopen = () => {
    send(sessWS, { type: 'attach', session_id: sessId });
    sizeTerminal();
  };
}

function onSessionFrame(f) {
  if (f.type === TYPE_OUTPUT) {
    if (f.sessionId === sessId) term.write(f.payload);
    return;
  }
  const env = parseControl(f.payload);
  if (env.type === 'set_button') {
    // Daemon restoring a custom binding on attach (see the daemon's attach
    // handler) — refresh the pad label so a rebind survives re-attach.
    setCustomLabel(env.action, env.text || '');
    return;
  }
  if (env.type === 'session_state') {
    // The session's process exited (or it was killed elsewhere) — leave the
    // session view and return to the list (which re-lists without it).
    if (env.status === 'exited' || env.status === 'killed') { leaveSession(); return; }
    // Live agent push: retitle the header when the agent running inside the
    // session changes (shell → claude → shell). Independent of the active flag.
    if (env.agent_label) setSessionTitle(env.agent_label);
    // Another client took the PTY size — park into Controller. active:true is
    // deliberately ignored (see setController); reclaiming is always manual.
    if (env.active === false) setController(true);
  }
  if (env.type === 'error') toast(env.message || 'error');
}

// --- wheel -> agent ------------------------------------------------------------
// An agent that owns the alternate screen leaves nothing to scroll: xterm's
// viewport is exactly the visible rows, and the daemon skips scrollback for such
// sessions (internal/daemon/replay.go). It does scroll its own conversation on
// mouse-wheel events — but xterm can't deliver them, because `disableStdin`
// (output-only: input goes through the pad and text bar) drops every mouse event
// before it reaches onData. So encode the wheel here and send it as input.
//
// Only when the agent asked for mouse tracking. Otherwise there IS real
// scrollback and the wheel keeps scrolling xterm's own viewport, unchanged. The
// same choice, made the same way, as the iOS drag->wheel pan.
let wheelAccum = 0;

function wheelTicks(e, cellH) {
  // deltaMode: 0 = pixels, 1 = lines, 2 = pages. Normalize to pixels so a
  // trackpad's pixel stream and a mouse's line jumps both land on cell counts.
  const px = e.deltaMode === 1 ? e.deltaY * cellH
    : e.deltaMode === 2 ? e.deltaY * cellH * term.rows
    : e.deltaY;
  wheelAccum += px;
  const n = Math.trunc(Math.abs(wheelAccum) / cellH);
  if (n === 0) return [];
  // Read the direction before consuming, so an exact consume-to-zero can't flip
  // the next tick. Wheel down = toward newer output, the native direction.
  const down = wheelAccum >= 0;
  wheelAccum -= (down ? 1 : -1) * n * cellH;
  return Array(n).fill(down ? 65 : 64);
}

function encodeWheel(button, col, row) {
  // SGR when the session negotiated ?1006h, X10 otherwise — the same agent
  // build negotiates differently on different machines, and X10 bytes sent to
  // an SGR session (or the reverse) land in the prompt as garbage. xterm makes
  // the tracking mode public but not the encoding, hence the one internal read;
  // xterm is vendored and pinned here, and the fallback is the safe default.
  const sgr = term._core?.coreMouseService?.activeEncoding === 'SGR';
  return sgr
    ? `\x1b[<${button};${col};${row}M`
    : `\x1b[M${String.fromCharCode(32 + button, 32 + col, 32 + row)}`;
}

// Capture phase is load-bearing: xterm's own wheel listener lives on the
// child screen element and, when mouse tracking is on, calls stopPropagation()
// (CoreMouseService cancel(e, true) → preventDefault + stopPropagation). A
// bubble-phase listener on this parent #term never sees the event, so the
// forwarding silently did nothing for the alt-screen agents that need it most.
// Capture runs ancestors before the child, so we get the wheel first and the
// stopPropagation below us is too late to matter. (With tracking off xterm
// takes a different branch that does not stop propagation, so the early return
// below still lets xterm scroll its own scrollback viewport as before.)
$('term').addEventListener('wheel', (e) => {
  if (!term || !sessId || term.modes.mouseTrackingMode === 'none') return;
  const box = $('term').getBoundingClientRect();
  if (!box.height || !term.rows || !term.cols) return;
  e.preventDefault(); // the agent scrolls instead of the page

  const cellH = box.height / term.rows;
  const ticks = wheelTicks(e, cellH);
  if (ticks.length === 0) return;
  // 1-based cell under the pointer, clamped to the grid.
  const col = Math.min(term.cols, Math.max(1, Math.ceil((e.clientX - box.left) / (box.width / term.cols))));
  const row = Math.min(term.rows, Math.max(1, Math.ceil((e.clientY - box.top) / cellH)));
  for (const button of ticks) {
    send(sessWS, { type: 'text_input', session_id: sessId, text: encodeWheel(button, col, row) });
  }
}, { passive: false, capture: true });

// --- touch drag -> wheel (phone) --------------------------------------------
// A phone has no wheel event, and an alt-screen agent (mouse tracking on) has
// no scrollback to pan either — so without this a one-finger drag does nothing
// on a phone. Translate the drag into the same wheel ticks the wheel handler
// sends, in natural-scroll direction (finger down pulls older content into
// view = wheel up), mirroring the iOS drag->wheel pan. Only while the agent is
// listening; with tracking off there is real scrollback and xterm's own touch
// panning handles it (we early-return on the same gate, and skip preventDefault
// so xterm's touchmove keeps working). Non-passive + capture so preventDefault
// lands before iOS commits a page-scroll gesture, and before xterm's child
// touch handlers (which are no-ops while tracking is on anyway).
let touchAccum = 0;
let touchLastY = 0;
let touchId = null;

function touchTicks(deltaY, cellH) {
  // One tick per whole cell of travel, carrying the sub-cell remainder across
  // moves so a slow drag still adds up. Read the direction before consuming so
  // an exact consume-to-zero can't flip the next move.
  touchAccum += deltaY;
  const n = Math.trunc(Math.abs(touchAccum) / cellH);
  if (n === 0) return [];
  const fingerDown = touchAccum >= 0;
  touchAccum -= (fingerDown ? 1 : -1) * n * cellH;
  return Array(n).fill(fingerDown ? 64 : 65); // down -> wheel up (older content)
}

function touchTrackingOn() {
  return !!term && !!sessId && term.modes.mouseTrackingMode !== 'none';
}

$('term').addEventListener('touchstart', (e) => {
  if (!touchTrackingOn() || e.touches.length !== 1) return;
  touchId = e.touches[0].identifier;
  touchAccum = 0;
  touchLastY = e.touches[0].clientY;
  e.preventDefault(); // stop iOS from committing a page-scroll gesture up front
}, { passive: false, capture: true });

$('term').addEventListener('touchmove', (e) => {
  if (!touchTrackingOn() || touchId === null) return;
  const t = Array.from(e.touches).find((c) => c.identifier === touchId);
  if (!t) return;
  const box = $('term').getBoundingClientRect();
  if (!box.height || !term.rows || !term.cols) return;
  e.preventDefault(); // we scroll the agent, not the page

  const cellH = box.height / term.rows;
  const deltaY = t.clientY - touchLastY;
  touchLastY = t.clientY;
  const ticks = touchTicks(deltaY, cellH);
  if (ticks.length === 0) return;
  const col = Math.min(term.cols, Math.max(1, Math.ceil((t.clientX - box.left) / (box.width / term.cols))));
  const row = Math.min(term.rows, Math.max(1, Math.ceil((t.clientY - box.top) / cellH)));
  for (const button of ticks) {
    send(sessWS, { type: 'text_input', session_id: sessId, text: encodeWheel(button, col, row) });
  }
}, { passive: false, capture: true });

function endTouchDrag() { touchId = null; touchAccum = 0; }
$('term').addEventListener('touchend', endTouchDrag, { passive: true, capture: true });
$('term').addEventListener('touchcancel', endTouchDrag, { passive: true, capture: true });

function sizeTerminal() {
  // parked: #term is display:none, so fit() would measure garbage — and a
  // non-owner's resize could claim an unowned session out from under the pad UI
  if (!fit || isController) return;
  fit.fit();
  send(sessWS, { type: 'resize', session_id: sessId, cols: term.cols, rows: term.rows });
}
window.addEventListener('resize', () => { if (!$('screen-session').hidden) sizeTerminal(); });
if (window.visualViewport) {
  window.visualViewport.addEventListener('resize', () => { if (!$('screen-session').hidden) sizeTerminal(); });
}

function leaveSession() {
  if (sessWS) { sessWS.onclose = null; sessWS.close(); sessWS = null; }
  sessId = 0;
  $('screen-session').hidden = true;
  $('screen-entry').hidden = false;
  entryConnect();
}
$('btn-back').onclick = leaveSession;

// pad buttons -> actions. A custom key bound to literal text prefills the text
// bar (so you can add args, e.g. "/compact focus on X"); keys/chords and every
// fixed button fire immediately.
// Brief highlight so a tap is visibly acknowledged (the CSS transition fades
// it back). :active alone is too fleeting on touch.
function flashTap(el) {
  el.classList.add('tapped');
  setTimeout(() => el.classList.remove('tapped'), 180);
}

for (const btn of document.querySelectorAll('#pad button')) {
  btn.addEventListener('click', () => {
    flashTap(btn);
    const action = btn.dataset.action;
    if (action === 'custom_1' || action === 'custom_2') {
      const val = custom[action];
      if (val && !isKeyBinding(val)) {
        const ti = $('text-input');
        ti.value = val.endsWith(' ') ? val : val + ' ';
        ti.focus();
        return;
      }
    }
    send(sessWS, { type: 'action', session_id: sessId, action });
  });
}

// text bar -> text_input with CR (Return submits in raw-mode agent TUIs)
function sendText() {
  const v = $('text-input').value;
  if (!v) return;
  send(sessWS, { type: 'text_input', session_id: sessId, text: v + '\r' });
  $('text-input').value = '';
}
$('text-send').onclick = sendText;
$('text-input').addEventListener('keydown', (e) => { if (e.key === 'Enter') sendText(); });

// --- rebind dialog (hold custom 1/2): a single key chord (modifiers + base) or
// literal text, mirroring the iOS builder. The daemon encodes chords like
// "Ctrl+Space"; anything that isn't a chord/named key is literal text.
const MODS = ['Ctrl', 'Alt', 'Shift'];
const QUICK_KEYS = ['Space', 'Tab', 'Enter', 'Esc', 'Up', 'Down', 'Left', 'Right'];
const NAMED_KEYS = ['Esc', 'Enter', 'Tab', 'Shift+Tab', 'Space', 'Up', 'Down', 'Left', 'Right'];
let rebindTarget = '';
const rebindMods = new Set();

function canonicalMod(m) {
  switch (m.toLowerCase()) {
    case 'ctrl': case 'control': case 'ctl': case 'c': return 'Ctrl';
    case 'alt': case 'opt': case 'option': case 'meta': case 'm': return 'Alt';
    case 'shift': return 'Shift';
    default: return m;
  }
}
function canonicalBase(b) {
  switch (b.toLowerCase()) {
    case 'space': return 'Space';
    case 'enter': case 'return': return 'Enter';
    case 'tab': return 'Tab';
    case 'esc': case 'escape': return 'Esc';
    case 'up': return 'Up'; case 'down': return 'Down';
    case 'left': return 'Left'; case 'right': return 'Right';
    default: return b;
  }
}
function normalizeChord(s) {
  const tokens = s.replace(/_/g, '+').split('+').filter(Boolean);
  if (tokens.length <= 1) return canonicalBase(tokens[0] || '');
  return [...tokens.slice(0, -1).map(canonicalMod), canonicalBase(tokens[tokens.length - 1])].join('+');
}
function isKeyBinding(s) {
  return s.includes('+') || NAMED_KEYS.includes(s);
}
// ⌘/Super/Win have no terminal byte encoding (the OS/terminal swallows them), so
// a chord using them would be sent to the agent as literal text. Flag it instead.
const UNSENDABLE_MODS = new Set(['cmd', 'command', '⌘', 'super', 'win', 'windows', 'gui', 'hyper']);
function unsendableMod(chord) {
  if (!chord.includes('+')) return null;
  const mods = chord.split('+').slice(0, -1);
  return mods.find((m) => UNSENDABLE_MODS.has(m.toLowerCase())) || null;
}
function composedChord() {
  const base = $('rebind-text').value.trim();
  if (!base) return '';
  const picked = MODS.filter((m) => rebindMods.has(m));
  return normalizeChord([...picked, base].join('+'));
}
function updateRebindPreview() {
  const c = composedChord();
  $('rebind-preview').textContent = c || '—';
  const bad = unsendableMod(c);
  $('rebind-warn').hidden = !bad;
  $('rebind-save').disabled = !c || !!bad;
}

(function buildRebindKeys() {
  const wrap = $('rebind-keys');
  for (const k of QUICK_KEYS) {
    const b = document.createElement('button');
    b.type = 'button';
    b.textContent = k;
    b.onclick = () => { $('rebind-text').value = k; updateRebindPreview(); };
    wrap.appendChild(b);
  }
})();

for (const b of $('rebind-mods').querySelectorAll('button')) {
  b.onclick = () => {
    const m = b.dataset.mod;
    if (rebindMods.has(m)) { rebindMods.delete(m); b.classList.remove('selected'); }
    else { rebindMods.add(m); b.classList.add('selected'); }
    updateRebindPreview();
  };
}
$('rebind-text').addEventListener('input', updateRebindPreview);

function openRebind(action) {
  rebindTarget = action;
  rebindMods.clear();
  for (const b of $('rebind-mods').querySelectorAll('button')) b.classList.remove('selected');
  // seed from the current binding: split a chord into mods + base, else text
  const cur = custom[action] || '';
  let base = cur;
  if (cur.includes('+') && !NAMED_KEYS.includes(cur)) {
    const t = cur.split('+');
    base = t[t.length - 1];
    for (const raw of t.slice(0, -1)) {
      const cm = canonicalMod(raw);
      if (MODS.includes(cm)) rebindMods.add(cm);
    }
    for (const b of $('rebind-mods').querySelectorAll('button')) {
      b.classList.toggle('selected', rebindMods.has(b.dataset.mod));
    }
  }
  $('rebind-text').value = base;
  $('rebind-title').textContent = `Rebind [${action === 'custom_1' ? 'Custom 1' : 'Custom 2'}]`;
  updateRebindPreview();
  $('rebind').showModal();
}

$('rebind-cancel').onclick = () => $('rebind').close();
$('rebind-save').onclick = () => {
  const value = composedChord();
  if (value && !unsendableMod(value)) {
    send(sessWS, { type: 'set_button', session_id: sessId, action: rebindTarget, text: value });
    setCustomLabel(rebindTarget, value); // optimistic; the daemon re-pushes it on re-attach
  }
  $('rebind').close();
};

// long-press (500ms) opens rebind; a normal tap still fires the action/prefill
function attachLongPress(btn, action) {
  let timer = null;
  let fired = false;
  const start = () => {
    fired = false;
    timer = setTimeout(() => { fired = true; openRebind(action); }, 500);
  };
  const cancel = () => clearTimeout(timer);
  btn.addEventListener('pointerdown', start);
  btn.addEventListener('pointerup', cancel);
  btn.addEventListener('pointerleave', cancel);
  btn.addEventListener('contextmenu', (e) => e.preventDefault());
  btn.addEventListener('click', (e) => { if (fired) e.stopImmediatePropagation(); }, true);
}
attachLongPress($('btn-custom1'), 'custom_1');
attachLongPress($('btn-custom2'), 'custom_2');

entryConnect();
