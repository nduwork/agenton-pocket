//go:build ignore

// ansidemo is a stub "agent" that emits ANSI styling and cursor movement so the
// TUI's headless emulator can be exercised end-to-end. It prints a colored
// header, clears the line, moves the cursor, then echoes typed lines prefixed
// with their ordinal. Run via: go run ansidemo.go
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	// Clear screen, set title, print a colored banner using SGR + cursor moves.
	fmt.Print("\033[2J\033[H")
	fmt.Print("\033]0;agenton-ansidemo\007")
	fmt.Print("\033[1;4;36m┌─ agenton demo ─┐\033[0m\r\n")
	fmt.Print("\033[32mready\033[0m — type something, enter to echo:\r\n")
	fmt.Print("> ")
	b := bufio.NewReader(os.Stdin)
	n := 0
	for {
		line, err := b.ReadString('\n')
		if err != nil {
			return
		}
		n++
		// Move cursor up one line, clear it, print a colored echo with count.
		fmt.Printf("\033[1A\033[2K\033[33m[%d]\033[0m echo: %s\r\n> ", n, line[:len(line)-1])
	}
}
