package node

import (
	"os"
	"testing"
)

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }
func removeAll(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
}
