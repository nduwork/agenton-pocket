package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func osStat(path string) (os.FileInfo, error) { return os.Stat(path) }

func osMkdirTemp(dir, pattern string) (string, error) { return os.MkdirTemp(dir, pattern) }

func osRemoveAll(path string) error { return os.RemoveAll(path) }
