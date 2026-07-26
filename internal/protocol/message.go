package protocol

import "encoding/json"

type MsgType string

const (
	MsgListSessions  MsgType = "list_sessions"
	MsgSessionList   MsgType = "session_list"
	MsgNewSession    MsgType = "new_session"
	MsgNewSessionCmd MsgType = "new_session_cmd"
	MsgAttach        MsgType = "attach"
	MsgDetach        MsgType = "detach"
	MsgKillSession   MsgType = "kill_session"
	MsgAction        MsgType = "action"
	// MsgSetButton rebinds a customizable button (Action = custom_1/custom_2,
	// Text = the key name, combo, or literal string it should send).
	MsgSetButton    MsgType = "set_button"
	MsgTextInput    MsgType = "text_input"
	MsgResize       MsgType = "resize"
	MsgSessionState MsgType = "session_state"
	// MsgSetActive (client → daemon): Active=true claims the shared PTY size for
	// this client and resizes immediately; Active=false releases it. The daemon
	// echoes ownership to every attached client via session_state.Active.
	MsgSetActive MsgType = "set_active"
	MsgError     MsgType = "error"
	// Directory browsing for the new-session cwd picker: the client asks for a
	// path's subdirectories, the daemon answers with the resolved path + names.
	MsgListDir MsgType = "list_dir"
	MsgDirList MsgType = "dir_list"
)

type SessionInfo struct {
	ID           uint32 `json:"id"`
	Name         string `json:"name"`
	Agent        string `json:"agent"`
	Status       string `json:"status"`
	Cwd          string `json:"cwd"`
	LastActivity int64  `json:"last_activity"`
	// Repo is a short display label for the session's working directory —
	// the git repo's top-level dir name, falling back to the folder name.
	// Clients show it in the session list instead of the full Cwd path.
	Repo string `json:"repo,omitempty"`
	// CommandLine is the full launch command ("ollama run glm-5.2:cloud"),
	// so clients can offer "start another one like this".
	CommandLine string `json:"command_line,omitempty"`
}

type Envelope struct {
	Type      MsgType       `json:"type"`
	SessionID uint32        `json:"session_id,omitempty"`
	Preset    string        `json:"preset,omitempty"`
	Action    string        `json:"action,omitempty"`
	Text      string        `json:"text,omitempty"`
	Status    string        `json:"status,omitempty"`
	// Active: on session_state (daemon → client) it is true iff the receiving
	// client currently owns the shared PTY size; on set_active (client → daemon)
	// it is the requested take(true)/release(false). Pointer so both true and
	// false serialize (omitempty would drop false).
	Active  *bool         `json:"active,omitempty"`
	Message string        `json:"message,omitempty"`
	Sessions  []SessionInfo `json:"sessions,omitempty"`
	// Unmanaged (session_list only): agent processes discovered on the host
	// that the daemon does NOT own — command line + cwd only. Clients may
	// offer them as "start a managed one like this"; they cannot be attached.
	Unmanaged []SessionInfo `json:"unmanaged,omitempty"`
	// Command-line session creation (MsgNewSessionCmd): Command is the binary
	// to run, Args its arguments, Cwd the working directory. Agent is a free
	// label shown in the entry list.
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	Cwd     string   `json:"cwd,omitempty"`
	Agent   string   `json:"agent_label,omitempty"`
	// Resize (MsgResize): new PTY dimensions in cells.
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`
	// Directory browsing (MsgListDir request / MsgDirList reply): Path is the
	// directory to list (request; empty = home) or the resolved absolute path
	// (reply); Dirs are the subdirectory names in that path.
	Path string   `json:"path,omitempty"`
	Dirs []string `json:"dirs,omitempty"`
}

func EncodeEnvelope(e Envelope) ([]byte, error) {
	return json.Marshal(e)
}

func DecodeEnvelope(b []byte) (Envelope, error) {
	var e Envelope
	err := json.Unmarshal(b, &e)
	return e, err
}
