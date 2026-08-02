package store

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLatestCheckpointIsMostRecentlyCreated pins "latest" to creation time.
// Ordering by turn number assumes turns are monotonic across activations, which
// is exactly the assumption that failed: a woken agent records lower turn
// numbers later in time.
func TestLatestCheckpointIsMostRecentlyCreated(t *testing.T) {
	s := tmpStore(t)
	mustCreate(t, s, "a1")
	ws, _ := s.EnsureWorkspace("a1")

	// First activation reached turn 5.
	if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte("as of turn 5"), 0o644); err != nil {
		t.Fatal(err)
	}
	high, err := s.SnapshotWorkspace("a1", 5, ws, "")
	if err != nil {
		t.Fatal(err)
	}
	// Second activation restarted its own count and recorded turn 2 later in
	// time. The most recently created checkpoint is the newer one.
	if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte("recorded later"), 0o644); err != nil {
		t.Fatal(err)
	}
	low, err := s.SnapshotWorkspace("a1", 2, ws, high.ID)
	if err != nil {
		t.Fatal(err)
	}

	latest, err := s.LatestCheckpointForAgent("a1")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.ID != low.ID {
		t.Fatalf("latest checkpoint = %s (turn %d), want the most recently created %s (turn %d)",
			latest.ID, latest.Turn, low.ID, low.Turn)
	}
}
