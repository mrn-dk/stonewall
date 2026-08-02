package node

import (
	"context"
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
