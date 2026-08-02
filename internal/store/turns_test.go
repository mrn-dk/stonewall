package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrn-dk/stonewall/internal/model"
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
	high, err := s.SnapshotWorkspace("a1", 5, 5, ws, "")
	if err != nil {
		t.Fatal(err)
	}
	// Second activation restarted its own count and recorded turn 2 later in
	// time. The most recently created checkpoint is the newer one.
	if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte("recorded later"), 0o644); err != nil {
		t.Fatal(err)
	}
	low, err := s.SnapshotWorkspace("a1", 2, 2, ws, high.ID)
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

// appendTurns appends n turns to an agent's log, each a couple of ordinary
// events followed by the boundary that closes it, passing the runtime's own
// (restarting) turn number.
func appendTurns(t *testing.T, s *Store, id string, runtimeTurns []int) {
	t.Helper()
	for _, rt := range runtimeTurns {
		if _, err := s.AppendEvent(id, "", model.EventLLMCall, rt, "", map[string]any{"turn": rt}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AppendEvent(id, "", model.EventTurnBoundary, rt, "", map[string]any{"turn": rt}); err != nil {
			t.Fatal(err)
		}
	}
}

// TestUnreachedTurnIsNotFound: asking for a turn the agent never reached must
// say so rather than hand back the nearest one. A quieter wrong answer is the
// same failure this change exists to remove.
func TestUnreachedTurnIsNotFound(t *testing.T) {
	s := tmpStore(t)
	mustCreate(t, s, "a1")
	appendTurns(t, s, "a1", []int{1, 2, 3})

	if _, err := s.ResolveTurn("a1", 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ResolveTurn(99) = %v, want ErrNotFound", err)
	}
	if _, err := s.CheckpointAtTurn("a1", 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CheckpointAtTurn(99) = %v, want ErrNotFound", err)
	}
	// Turn 0 is not a turn.
	if _, err := s.ResolveTurn("a1", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ResolveTurn(0) = %v, want ErrNotFound", err)
	}
	// A sequence that is not a turn boundary is not an address either.
	b, err := s.ResolveTurn("a1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveSeq("a1", b.Seq-1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ResolveSeq(non-boundary) = %v, want ErrNotFound", err)
	}
}

// TestEventsWithinATurnShareItsOrdinal pins the counting rule: everything in a
// turn carries its ordinal, the boundary closes it, and the next event carries
// one greater — regardless of what the writer numbered them.
func TestEventsWithinATurnShareItsOrdinal(t *testing.T) {
	s := tmpStore(t)
	mustCreate(t, s, "a1")
	// A runtime whose own counter restarts at 1 for its second activation.
	appendTurns(t, s, "a1", []int{1, 2, 1, 2})

	evs, err := s.ReadEvents("a1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{1, 1, 2, 2, 3, 3, 4, 4}
	if len(evs) != len(want) {
		t.Fatalf("got %d events, want %d", len(evs), len(want))
	}
	for i, e := range evs {
		if e.Turn != want[i] {
			t.Fatalf("event %d (%s) ordinal = %d, want %d", i, e.Kind, e.Turn, want[i])
		}
	}
	// The next event opens the turn after the last boundary.
	next, err := s.AppendEvent("a1", "", model.EventMessage, 2, "", map[string]any{"role": "assistant"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Turn != 5 {
		t.Fatalf("event after the 4th boundary has ordinal %d, want 5", next.Turn)
	}
	// The writer's own number is retained as run-relative information.
	if got := string(next.Payload); !strings.Contains(got, `"runtime_turn":2`) {
		t.Fatalf("payload %s does not retain the runtime's turn number", got)
	}
}
