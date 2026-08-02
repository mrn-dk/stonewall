package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrn-dk/stonewall/internal/model"
)

// writeLegacyLog writes a JSONL log in the shape an earlier version produced:
// two activations whose turn numbers both run 1,2, so the log contains two
// events claiming turn 1 and two claiming turn 2.
func writeLegacyLog(t *testing.T, s *Store, agentID string, events []model.Event) {
	t.Helper()
	path := s.eventPath(agentID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for i := range events {
		if err := enc.Encode(&events[i]); err != nil {
			t.Fatal(err)
		}
	}
}

// TestReconcileRecountsCollidingHistoricalTurns walks a log written before the
// ordinal was counted: its turn numbers collide across activations. After
// reconciliation the ordinals ascend cleanly over the whole log, last_turn
// counts them, and every checkpoint resolves to the boundary it was taken at.
func TestReconcileRecountsCollidingHistoricalTurns(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, "a1")
	ws, _ := s.EnsureWorkspace("a1")

	// Four checkpoints with distinct content, recorded the old way: no boundary
	// seq, and turn numbers that restart with the activation.
	legacyTurns := []int{1, 2, 1, 2}
	ids := make([]string, 4)
	for i, lt := range legacyTurns {
		// Distinct lengths, so the incremental snapshot does not treat the file
		// as unchanged and the four checkpoints get four distinct digests.
		if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte(strings.Repeat("x", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		parent := ""
		if i > 0 {
			parent = ids[i-1]
		}
		cp, err := s.SnapshotWorkspace("a1", lt, 0, ws, parent)
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = cp.ID
	}

	// The log: per turn a model call, the boundary, then the checkpoint event.
	var events []model.Event
	var seq uint64
	base := time.Now().UTC()
	boundarySeq := make([]uint64, 4)
	for i, lt := range legacyTurns {
		act := "act-1"
		if i >= 2 {
			act = "act-2"
		}
		add := func(kind model.EventKind, payload any) {
			seq++
			b, _ := json.Marshal(payload)
			events = append(events, model.Event{
				Seq: seq, AgentID: "a1", ActivationID: act, Kind: kind,
				OccurredAt: base.Add(time.Duration(seq) * time.Second),
				Turn:       lt, Durability: model.DurabilityFleet, Payload: b,
			})
		}
		add(model.EventLLMCall, map[string]any{"turn": lt})
		add(model.EventTurnBoundary, map[string]any{"turn": lt})
		boundarySeq[i] = seq
		add(model.EventCheckpoint, map[string]any{"checkpoint_id": ids[i], "turn": lt})
	}
	writeLegacyLog(t, s, "a1", events)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen: startup reconciliation recounts the index from the log.
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	a, err := s2.GetAgent("a1")
	if err != nil {
		t.Fatal(err)
	}
	if a.LastTurn != 4 {
		t.Fatalf("last_turn after reconcile = %d, want 4 (the log holds four turn boundaries)", a.LastTurn)
	}

	// Ordinals ascend cleanly over the whole log, colliding history and all.
	boundaries, err := s2.TurnBoundaries("a1")
	if err != nil {
		t.Fatal(err)
	}
	if len(boundaries) != 4 {
		t.Fatalf("got %d turn boundaries, want 4", len(boundaries))
	}
	for i, b := range boundaries {
		if b.Turn != i+1 || b.Seq != boundarySeq[i] {
			t.Fatalf("boundary %d = %+v, want turn %d seq %d", i, b, i+1, boundarySeq[i])
		}
	}

	// Every checkpoint was backfilled with the boundary it was produced at, and
	// each turn resolves to its own checkpoint rather than a colliding one.
	for i := range ids {
		cp, err := s2.CheckpointAtTurn("a1", i+1)
		if err != nil {
			t.Fatalf("turn %d does not resolve: %v", i+1, err)
		}
		if cp.ID != ids[i] {
			t.Fatalf("turn %d resolved to checkpoint %s, want %s", i+1, cp.ID, ids[i])
		}
		if cp.BoundarySeq != boundarySeq[i] {
			t.Fatalf("checkpoint %d boundary_seq = %d, want %d", i+1, cp.BoundarySeq, boundarySeq[i])
		}
		if cp.Turn != i+1 {
			t.Fatalf("checkpoint %d turn = %d, want %d", i+1, cp.Turn, i+1)
		}
	}

	// A new append continues the counted ordinal rather than restarting.
	e, err := s2.AppendEvent("a1", "act-3", model.EventTurnBoundary, 1, "", map[string]any{"turn": 1})
	if err != nil {
		t.Fatal(err)
	}
	if e.Turn != 5 {
		t.Fatalf("first turn after reconcile has ordinal %d, want 5", e.Turn)
	}
}
