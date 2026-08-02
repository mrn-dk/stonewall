package node

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"testing"

	"github.com/mrn-dk/stonewall/internal/model"
	"github.com/mrn-dk/stonewall/internal/store"
)

// runTwoActivations runs two activations of MOCK_TURNS turns each for a fresh
// agent, which is the shape that exposed cumulative-numbering bugs: the second
// activation's runtime counts from 1 again.
func runTwoActivations(t *testing.T, n *Node, s *store.Store, id string, policy model.CheckpointPolicy) {
	t.Helper()
	createAgent(t, s, id, policy)
	if _, err := n.Activate(context.Background(), id); err != nil {
		t.Fatalf("activation 1: %v", err)
	}
	if _, err := s.EnqueueInput(id, "continue", "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Activate(context.Background(), id); err != nil {
		t.Fatalf("activation 2: %v", err)
	}
}

// turnOrdinals returns the turn ordinal stamped on each turn-boundary event, in
// log order.
func turnOrdinals(t *testing.T, s *store.Store, id string) []int {
	t.Helper()
	evs, err := s.ReadEvents(id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []int
	for _, e := range evs {
		if e.Kind == model.EventTurnBoundary {
			out = append(out, e.Turn)
		}
	}
	return out
}

// TestTurnOrdinalsCumulativeAcrossActivations is the bug, stated as a test: the
// durable log's turn ordinals must be unique and strictly increasing over the
// agent's whole life, not restart at 1 on every wake.
func TestTurnOrdinalsCumulativeAcrossActivations(t *testing.T) {
	n, s := newTestNode(t)
	t.Setenv("MOCK_TURNS", "2")
	runTwoActivations(t, n, s, "a1", model.CheckpointPerTurn)

	ordinals := turnOrdinals(t, s, "a1")
	if len(ordinals) != 4 {
		t.Fatalf("want 4 turn boundaries over two activations, got %d (%v)", len(ordinals), ordinals)
	}
	prev := 0
	for i, got := range ordinals {
		if got <= prev {
			t.Fatalf("turn ordinals must be strictly increasing: %v (index %d)", ordinals, i)
		}
		if got != i+1 {
			t.Fatalf("turn ordinal %d = %d, want %d (ordinals %v)", i, got, i+1, ordinals)
		}
		prev = got
	}
}

// TestLastTurnCountsBoundariesAndNeverDecreases observes the recorded turn count
// at each activation boundary. It must count the agent's turn boundaries — a
// value that is incremented cannot regress, and cannot stall either.
func TestLastTurnCountsBoundariesAndNeverDecreases(t *testing.T) {
	n, s := newTestNode(t)
	t.Setenv("MOCK_TURNS", "2")
	createAgent(t, s, "a1", model.CheckpointPerTurn)

	var observed []int
	observe := func(when string) int {
		a, err := s.GetAgent("a1")
		if err != nil {
			t.Fatalf("%s: %v", when, err)
		}
		if len(observed) > 0 && a.LastTurn < observed[len(observed)-1] {
			t.Fatalf("last_turn decreased at %s: %v -> %d", when, observed, a.LastTurn)
		}
		observed = append(observed, a.LastTurn)
		return a.LastTurn
	}

	observe("before activation 1")
	if _, err := n.Activate(context.Background(), "a1"); err != nil {
		t.Fatal(err)
	}
	if got := observe("after activation 1"); got != 2 {
		t.Fatalf("after activation 1: last_turn = %d, want 2", got)
	}
	if _, err := s.EnqueueInput("a1", "continue", "user"); err != nil {
		t.Fatal(err)
	}
	observe("before activation 2")
	if _, err := n.Activate(context.Background(), "a1"); err != nil {
		t.Fatal(err)
	}
	if got := observe("after activation 2"); got != 4 {
		t.Fatalf("after activation 2: last_turn = %d, want 4 (it counts turn boundaries, %v)", got, observed)
	}
	if n := len(turnOrdinals(t, s, "a1")); n != observed[len(observed)-1] {
		t.Fatalf("last_turn %d does not match %d turn boundaries in the log", observed[len(observed)-1], n)
	}
}

// workspaceFiles lists the file names in a workspace directory.
func workspaceFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read workspace %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// TestForkTargetsTheRightActivationsTurn forks at a turn from the *first*
// activation of an agent that has run two. The second activation numbered its
// own turns 1 and 2 again and rewrote turn_1.txt, so a fork that resolves by
// the runtime's number lands on the wrong point in history — with a workspace
// that has one file too many and the wrong bytes in the file it does have.
func TestForkTargetsTheRightActivationsTurn(t *testing.T) {
	n, s := newTestNode(t)
	t.Setenv("MOCK_TURNS", "2")
	createAgent(t, s, "parent", model.CheckpointPerTurn)

	if _, err := n.Activate(context.Background(), "parent"); err != nil {
		t.Fatal(err)
	}
	// What turn 1 of the first activation actually wrote.
	firstTurnFile, err := readTurnFile(s.WorkspacePath("parent"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueInput("parent", "continue", "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Activate(context.Background(), "parent"); err != nil {
		t.Fatal(err)
	}
	// The second activation rewrote turn_1.txt, so the two are distinguishable.
	rewritten, err := readTurnFile(s.WorkspacePath("parent"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(rewritten) == string(firstTurnFile) {
		t.Skip("the second activation happened to rewrite an identical file; nothing to distinguish")
	}

	child, err := n.Fork("parent", 1)
	if err != nil {
		t.Fatalf("fork at turn 1: %v", err)
	}
	if child.ParentTurn != 1 {
		t.Fatalf("child parent_turn = %d, want 1", child.ParentTurn)
	}
	ws := s.WorkspacePath(child.ID)
	if files := workspaceFiles(t, ws); len(files) != 1 || files[0] != "turn_1.txt" {
		t.Fatalf("fork at turn 1 has workspace %v, want just turn_1.txt (a later turn's checkpoint has more)", files)
	}
	got, err := readTurnFile(ws, 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(firstTurnFile) {
		t.Fatalf("fork at turn 1 got the second activation's turn 1:\n got %q\nwant %q", got, firstTurnFile)
	}

	// Turn 3 is the second activation's first turn: same runtime number, later
	// point in history, and its own workspace.
	child3, err := n.Fork("parent", 3)
	if err != nil {
		t.Fatalf("fork at turn 3: %v", err)
	}
	if files := workspaceFiles(t, s.WorkspacePath(child3.ID)); len(files) != 2 {
		t.Fatalf("fork at turn 3 has workspace %v, want the two files the agent had by then", files)
	}
	// A turn the parent never reached is not-found, not the nearest one.
	if _, err := n.Fork("parent", 99); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("fork at turn 99 = %v, want ErrNotFound", err)
	}
}

// TestRestoreTargetsTheRightActivationsTurn restores the live workspace to a
// turn from the first activation. Restore is the sharpest case: it rewrites the
// workspace from whatever the lookup returned and reports success.
func TestRestoreTargetsTheRightActivationsTurn(t *testing.T) {
	n, s := newTestNode(t)
	t.Setenv("MOCK_TURNS", "2")
	createAgent(t, s, "a1", model.CheckpointPerTurn)

	if _, err := n.Activate(context.Background(), "a1"); err != nil {
		t.Fatal(err)
	}
	firstTurnFile, err := readTurnFile(s.WorkspacePath("a1"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueInput("a1", "continue", "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Activate(context.Background(), "a1"); err != nil {
		t.Fatal(err)
	}

	// Restore to turn 1 — the same path the restore endpoint takes.
	cp, err := s.CheckpointAtTurn("a1", 1)
	if err != nil {
		t.Fatalf("resolve turn 1: %v", err)
	}
	if err := s.MaterializeWorkspace("a1", cp); err != nil {
		t.Fatal(err)
	}
	ws := s.WorkspacePath("a1")
	if files := workspaceFiles(t, ws); len(files) != 1 || files[0] != "turn_1.txt" {
		t.Fatalf("restored workspace = %v, want just turn_1.txt", files)
	}
	got, err := readTurnFile(ws, 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(firstTurnFile) {
		t.Fatalf("restore to turn 1 produced the second activation's turn 1:\n got %q\nwant %q", got, firstTurnFile)
	}
}

// TestResumeRestoresFromTheCheckpointOfItsActualLastTurn: a woken agent picks up
// from where its history says it is, not from a same-numbered earlier turn.
func TestResumeRestoresFromTheCheckpointOfItsActualLastTurn(t *testing.T) {
	n, s := newTestNode(t)
	t.Setenv("MOCK_TURNS", "2")
	runTwoActivations(t, n, s, "a1", model.CheckpointPerTurn)

	a, err := s.GetAgent("a1")
	if err != nil {
		t.Fatal(err)
	}
	if a.LastTurn != 4 {
		t.Fatalf("last_turn = %d, want 4", a.LastTurn)
	}
	atLast, err := s.CheckpointAtTurn("a1", a.LastTurn)
	if err != nil {
		t.Fatalf("resolve last turn %d: %v", a.LastTurn, err)
	}
	if atLast.ID != a.LastCheckpointID {
		t.Fatalf("the checkpoint at turn %d is %s but resume would restore %s",
			a.LastTurn, atLast.ID, a.LastCheckpointID)
	}

	// Wipe the workspace so the third activation must restore it.
	removeAll(t, s.WorkspacePath("a1"))
	if _, err := s.EnqueueInput("a1", "again", "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Activate(context.Background(), "a1"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if ordinals := turnOrdinals(t, s, "a1"); len(ordinals) != 6 || ordinals[5] != 6 {
		t.Fatalf("after a third activation the ordinals are %v, want 1..6", ordinals)
	}
	a, _ = s.GetAgent("a1")
	if a.LastTurn != 6 {
		t.Fatalf("last_turn after three activations = %d, want 6", a.LastTurn)
	}
}

// TestCheckpointEventNamesItsBoundary: the checkpoint event is appended after
// the boundary it belongs to, so it carries the boundary's turn and seq in its
// payload — which is what the dashboard's timeline labels a checkpoint with.
func TestCheckpointEventNamesItsBoundary(t *testing.T) {
	n, s := newTestNode(t)
	t.Setenv("MOCK_TURNS", "2")
	runTwoActivations(t, n, s, "a1", model.CheckpointPerTurn)

	evs, err := s.ReadEvents("a1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var turns []int
	for _, e := range evs {
		if e.Kind != model.EventCheckpoint {
			continue
		}
		var p struct {
			Turn        int    `json:"turn"`
			BoundarySeq uint64 `json:"boundary_seq"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if p.BoundarySeq == 0 || p.BoundarySeq >= e.Seq {
			t.Fatalf("checkpoint at seq %d names boundary seq %d", e.Seq, p.BoundarySeq)
		}
		turns = append(turns, p.Turn)
	}
	want := []int{1, 2, 3, 4}
	if len(turns) != len(want) {
		t.Fatalf("checkpoint turns = %v, want %v", turns, want)
	}
	for i := range want {
		if turns[i] != want[i] {
			t.Fatalf("checkpoint turns = %v, want %v", turns, want)
		}
	}
}
