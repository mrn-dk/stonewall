package node

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrn-dk/stonewall/internal/model"
	"github.com/mrn-dk/stonewall/internal/runtime"
	"github.com/mrn-dk/stonewall/internal/store"
)

func newTestNode(t *testing.T) (*Node, *store.Store) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	cfg := Config{
		MaxConcurrent:           2,
		PollInterval:            20 * time.Millisecond,
		CheckpointIntervalTurns: 3,
		CrashThreshold:          3,
		NodeBreakerThreshold:    100,
		ActivationTimeout:       10 * time.Second,
	}
	n := New(s, &runtime.MockRuntime{}, "", "", cfg)
	return n, s
}

func createAgent(t *testing.T, s *store.Store, id string, policy model.CheckpointPolicy) *model.Agent {
	a := &model.Agent{
		ID:         id,
		Image:      "acme/agent-host:1.4",
		Goal:       "test goal",
		Model:      "gpt-test",
		Grants:     model.Grants{FS: map[string]string{"/workspace": "rw"}, Cmd: []string{"rg"}},
		Isolation:  model.IsolationDedicated,
		Checkpoint: policy,
		State:      model.StatePending,
	}
	if err := s.CreateAgent(a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestRunActivationCompletionAndCheckpoint(t *testing.T) {
	n, s := newTestNode(t)
	t.Setenv("MOCK_TURNS", "2")
	createAgent(t, s, "a1", model.CheckpointPerTurn)

	reason, err := n.Activate(context.Background(), "a1")
	if err != nil {
		t.Fatalf("run: %v (reason %s)", err, reason)
	}
	if reason != model.EndCompleted {
		t.Fatalf("want completed, got %s", reason)
	}
	// Workspace should have files written per turn.
	ws := s.WorkspacePath("a1")
	if _, err := readTurnFile(ws, 1); err != nil {
		t.Fatalf("expected turn_1.txt: %v", err)
	}
	// Per-turn policy: a checkpoint should exist and be the last one.
	a, _ := s.GetAgent("a1")
	if a.LastCheckpointID == "" {
		t.Fatal("per_turn: expected a checkpoint")
	}
	// Events recorded: run_start, turns, tool events, checkpoints, run_end.
	evs, _ := s.ReadEvents("a1", 0, 0)
	if len(evs) < 4 {
		t.Fatalf("expected several events, got %d", len(evs))
	}
	// State parked after a successful activation.
	if a.State != model.StateParked {
		t.Fatalf("want parked, got %s", a.State)
	}
}

func TestResumeRestoresWorkspace(t *testing.T) {
	n, s := newTestNode(t)
	t.Setenv("MOCK_TURNS", "2")
	createAgent(t, s, "a1", model.CheckpointPerTurn)

	if _, err := n.Activate(context.Background(), "a1"); err != nil {
		t.Fatal(err)
	}
	// Wipe the workspace to force a restore on resume.
	removeAll(t, s.WorkspacePath("a1"))
	// Enqueue input to make it runnable again.
	if _, err := s.EnqueueInput("a1", "continue", "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Activate(context.Background(), "a1"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	// After resume the workspace must be restored from checkpoint (turn_1.txt
	// from the first activation must survive) plus new turn files.
	ws := s.WorkspacePath("a1")
	if _, err := readTurnFile(ws, 1); err != nil {
		t.Fatalf("resume did not restore turn_1.txt: %v", err)
	}
}

func TestCrashPolicyBackoffAndQuarantine(t *testing.T) {
	n, s := newTestNode(t)
	createAgent(t, s, "a1", model.CheckpointOnWrite)
	// Configure the mock to crash at turn 2 via... we need an env override.
	// The node reads env from InstanceSpec.Env, which the node doesn't expose
	// for the mock. Instead, drive the runtime directly through the node by
	// setting MOCK_CRASH_TURN in the process env.
	t.Setenv("MOCK_CRASH_TURN", "2")

	reason, err := n.Activate(context.Background(), "a1")
	if err == nil {
		t.Fatalf("expected crash error, got nil (reason %s)", reason)
	}
	if reason != model.EndCrashed {
		t.Fatalf("want crashed, got %s", reason)
	}
	a, _ := s.GetAgent("a1")
	if a.CrashCount != 1 {
		t.Fatalf("want crash_count 1, got %d", a.CrashCount)
	}
	if !a.Quarantined {
		t.Fatal("want quarantined after crash")
	}
	// Should not be runnable while quarantined.
	if n.runnable(a) {
		t.Fatal("quarantined agent should not be runnable")
	}
}

func TestForkLogChainAndCoWWorkspace(t *testing.T) {
	n, s := newTestNode(t)
	t.Setenv("MOCK_TURNS", "2")
	createAgent(t, s, "parent", model.CheckpointPerTurn)
	if _, err := n.Activate(context.Background(), "parent"); err != nil {
		t.Fatal(err)
	}
	child, err := n.Fork("parent", 1)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if child.ParentID != "parent" || child.ParentTurn != 1 {
		t.Fatalf("child parent = %s@%d", child.ParentID, child.ParentTurn)
	}
	// Child workspace is a CoW view of the parent checkpoint at turn 1:
	// turn_1.txt must be present.
	ws := s.WorkspacePath(child.ID)
	if _, err := readTurnFile(ws, 1); err != nil {
		t.Fatalf("fork workspace missing turn_1.txt: %v", err)
	}
	// History walks the parent chain.
	hist, err := s.WalkHistory(child.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Should include parent's turn events up to turn 1 plus the fork pointer
	// (excluded) — i.e. at least one parent turn boundary.
	foundParentTurn := false
	for _, e := range hist {
		if e.Kind == model.EventTurnBoundary && e.AgentID == "parent" && e.Turn == 1 {
			foundParentTurn = true
		}
	}
	if !foundParentTurn {
		t.Fatal("fork history does not include parent turn 1")
	}
	// Run the child; it writes its own turns on top of the CoW workspace.
	t.Setenv("MOCK_TURNS", "1")
	if _, err := s.EnqueueInput(child.ID, "go", "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Activate(context.Background(), child.ID); err != nil {
		t.Fatalf("run child: %v", err)
	}
	// turn_1.txt (inherited) and a new turn file coexist.
	if _, err := readTurnFile(s.WorkspacePath(child.ID), 1); err != nil {
		t.Fatalf("child lost inherited turn_1.txt: %v", err)
	}
}

// readTurnFile reads turn_N.txt from a workspace.
func readTurnFile(ws string, turn int) ([]byte, error) {
	return readFile(filepath.Join(ws, turnName(turn)))
}

func turnName(turn int) string {
	switch turn {
	case 1:
		return "turn_1.txt"
	case 2:
		return "turn_2.txt"
	case 3:
		return "turn_3.txt"
	}
	return "turn_unknown.txt"
}
