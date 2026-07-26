package main

import (
	"io"
	"os"
)

func osStdin() io.Reader  { return os.Stdin }
func osStdout() io.Writer { return os.Stdout }
