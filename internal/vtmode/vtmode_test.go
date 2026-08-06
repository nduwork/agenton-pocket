package vtmode

import "testing"

func TestUpdatePrivateModes(t *testing.T) {
	modes := map[int]bool{}
	UpdatePrivateModes([]byte("\x1b[?1049h\x1b[?1006hhello\x1b[?25l"), modes)
	if !modes[1049] || !modes[1006] || modes[25] {
		t.Fatalf("after set/reset: %v", modes)
	}
	// Reset 1049; 1006 stays on.
	UpdatePrivateModes([]byte("\x1b[?1049l"), modes)
	if modes[1049] || !modes[1006] {
		t.Fatalf("after 1049 reset: %v", modes)
	}
}

func TestUpdatePrivateModesMultipleParams(t *testing.T) {
	modes := map[int]bool{}
	UpdatePrivateModes([]byte("\x1b[?1000;1002;1003h"), modes)
	for _, n := range []int{1000, 1002, 1003} {
		if !modes[n] {
			t.Fatalf("%d not set: %v", n, modes)
		}
	}
}

// A non-mode CSI (SGR color) and bare text must not register as modes.
func TestUpdatePrivateModesIgnoresNonModes(t *testing.T) {
	modes := map[int]bool{}
	UpdatePrivateModes([]byte("plain \x1b[31mred\x1b[m text"), modes)
	if len(modes) != 0 {
		t.Fatalf("non-mode input set modes: %v", modes)
	}
}
