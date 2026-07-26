package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nduwork/agenton-pocket/internal/protocol"
)

// Navigating the list (up/down) must not dismiss the status notice; only an
// action key clears it.
func TestEntryNavigationKeepsNotice(t *testing.T) {
	e := entryModel{width: 80, notice: "detached — session keeps running",
		sessions: []protocol.SessionInfo{{ID: 1}, {ID: 2}}}

	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyDown})
	if e.notice == "" {
		t.Fatal("down-arrow cleared the status notice")
	}
	if e.cursor != 1 {
		t.Fatalf("down-arrow should move cursor to 1, got %d", e.cursor)
	}

	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if e.notice != "" {
		t.Fatalf("action key should clear the notice, got %q", e.notice)
	}
}

// The entry screen lists sessions by repo · terminal name (not full paths) and
// offers attach/new/kill.
func TestEntryViewListsSessions(t *testing.T) {
	e := entryModel{width: 80, sessions: []protocol.SessionInfo{
		{ID: 1, Name: "claude", Agent: "claude", Status: "running", Repo: "agenton"},
		{ID: 2, Name: "codex", Agent: "codex", Status: "running", Repo: "api"},
	}}
	v := e.View()
	for _, want := range []string{"agenton", "#1", "agenton", "api", "attach", "new", "kill"} {
		if !strings.Contains(v, want) {
			t.Errorf("entry view missing %q\n---\n%s", want, v)
		}
	}
}

// Empty list still renders the new/quit affordances rather than a blank screen.
func TestEntryViewEmpty(t *testing.T) {
	e := entryModel{width: 80}
	v := e.View()
	if !strings.Contains(v, "no sessions") || !strings.Contains(v, "new session") {
		t.Errorf("empty entry view missing prompts:\n%s", v)
	}
}
