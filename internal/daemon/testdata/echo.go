//go:build ignore

package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	fmt.Print("> ")
	r := bufio.NewReader(os.Stdin)
	for {
		line, err := readLine(r)
		if line != "" {
			fmt.Printf("echo: %s\n", line)
			fmt.Print("> ")
		}
		if err != nil {
			return
		}
	}
}

// readLine reads up to the next CR, LF, or CRLF terminator so the stub works
// regardless of whether the daemon sends a cooked shell newline (LF after
// ICRNL) or a raw Return (CR) — see the Enter/CR note in session.go.
func readLine(r *bufio.Reader) (string, error) {
	var b []byte
	for {
		c, err := r.ReadByte()
		if err != nil {
			return string(b), err
		}
		switch c {
		case '\r':
			// Consume a following LF, if any (CRLF).
			if nb, e := r.ReadByte(); e == nil {
				if nb != '\n' {
					_ = r.UnreadByte()
				}
			}
			return string(b), nil
		case '\n':
			return string(b), nil
		default:
			b = append(b, c)
		}
	}
}