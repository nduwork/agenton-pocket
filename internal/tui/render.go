package tui

// Phase 1 rendered PTY output by stripping ANSI and accumulating lines
// (appendOutput). Full VT emulation is now handled by the headless emulator
// in session.go, so this file holds no rendering helpers. Kept as a placeholder
// for future render-time post-processing (e.g. scrollback search highlights).
