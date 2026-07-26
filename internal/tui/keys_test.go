package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEncodeTeaKey(t *testing.T) {
	cases := []struct {
		k    tea.KeyMsg
		want string
	}{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")}, "hi"},
		{tea.KeyMsg{Type: tea.KeyEnter}, "\r"},
		{tea.KeyMsg{Type: tea.KeyEsc}, "\x1b"},
		{tea.KeyMsg{Type: tea.KeyBackspace}, "\x7f"},
		{tea.KeyMsg{Type: tea.KeyUp}, "\x1b[A"},
		{tea.KeyMsg{Type: tea.KeyShiftTab}, "\x1b[Z"},
		{tea.KeyMsg{Type: tea.KeyCtrlC}, "\x03"},
		{tea.KeyMsg{Type: tea.KeyCtrlO}, "\x0f"},
	}
	for _, c := range cases {
		if got := string(encodeTeaKey(c.k)); got != c.want {
			t.Errorf("encodeTeaKey(%v) = %q, want %q", c.k, got, c.want)
		}
	}
}
