package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/mrn-dk/stonewall/internal/model"
	"github.com/mrn-dk/stonewall/internal/node"
	"github.com/mrn-dk/stonewall/internal/runtime"
	"github.com/mrn-dk/stonewall/internal/store"
)

// newServerTwoActivations builds an agent that has run two activations of two
// turns each. Its runtime counted 1,2 both times; the log holds turns 1..4.
func newServerTwoActivations(t *testing.T) (*store.Store, string) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	t.Setenv("MOCK_TURNS", "2")
	n := node.New(s, &runtime.MockRuntime{}, "", "", node.Config{
		MaxConcurrent: 2, PollInterval: 10 * time.Second, ActivationTimeout: 10 * time.Second,
		CrashThreshold: 3, NodeBreakerThreshold: 100,
	})
	srv := New(":0", s, n, s.Root(), nil)
	ts := httptest.NewServer(srv.srv.Handler)
	t.Cleanup(ts.Close)

	a := &model.Agent{
		ID: "agt-two", Image: "img", Goal: "g", Model: "m",
		Grants:    model.Grants{FS: map[string]string{"/workspace": "rw"}, Cmd: []string{"rg"}},
		Isolation: model.IsolationDedicated, Checkpoint: model.CheckpointPerTurn, State: model.StatePending,
	}
	if err := s.CreateAgent(a); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Activate(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueInput(a.ID, "continue", "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Activate(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	return s, ts.URL
}

type browseResponse struct {
	CheckpointID string `json:"checkpoint_id"`
	Turn         int    `json:"turn"`
	BoundarySeq  uint64 `json:"boundary_seq"`
	Files        []struct {
		Path string `json:"path"`
	} `json:"files"`
}

func browse(t *testing.T, base, path string) (browseResponse, int) {
	t.Helper()
	resp := get(t, base, path)
	defer resp.Body.Close()
	var out browseResponse
	if resp.StatusCode == 200 {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return out, resp.StatusCode
}

// TestAddressingByTurnAndBySeqAgree: a turn ordinal and the sequence of that
// turn's boundary are two names for one point in the log, so they must return
// the same checkpoint and the same bytes.
func TestAddressingByTurnAndBySeqAgree(t *testing.T) {
	s, base := newServerTwoActivations(t)

	at, err := s.ResolveTurn("agt-two", 3)
	if err != nil {
		t.Fatalf("resolve turn 3: %v", err)
	}
	byTurn, code := browse(t, base, "/v1/agents/agt-two/workspace?at_turn=3")
	if code != 200 {
		t.Fatalf("browse by turn: %d", code)
	}
	bySeq, code := browse(t, base, "/v1/agents/agt-two/workspace?seq="+strconv.FormatUint(at.Seq, 10))
	if code != 200 {
		t.Fatalf("browse by seq: %d", code)
	}
	if byTurn.CheckpointID != bySeq.CheckpointID {
		t.Fatalf("turn 3 -> %s but seq %d -> %s", byTurn.CheckpointID, at.Seq, bySeq.CheckpointID)
	}
	// The checkpoint returned is one produced at or before that boundary — the
	// same rule that makes a turn without its own checkpoint resolve backwards.
	if byTurn.BoundarySeq > at.Seq {
		t.Fatalf("checkpoint boundary_seq = %d, which is after the turn boundary at seq %d", byTurn.BoundarySeq, at.Seq)
	}

	// Same contents, not merely the same id.
	fileByTurn := readCheckpointFileVia(t, base, byTurn.CheckpointID, "turn_1.txt")
	fileBySeq := readCheckpointFileVia(t, base, bySeq.CheckpointID, "turn_1.txt")
	if fileByTurn != fileBySeq {
		t.Fatalf("contents differ: %q vs %q", fileByTurn, fileBySeq)
	}

	// Repeating either call resolves to the same place.
	again, _ := browse(t, base, "/v1/agents/agt-two/workspace?at_turn=3")
	if again.CheckpointID != byTurn.CheckpointID {
		t.Fatalf("turn 3 is not stable: %s then %s", byTurn.CheckpointID, again.CheckpointID)
	}
}

// TestBrowseUnreachedTurnIsNotFound: the agent completed four turns, so turn 99
// is not-found and no nearby turn is substituted.
func TestBrowseUnreachedTurnIsNotFound(t *testing.T) {
	_, base := newServerTwoActivations(t)
	if _, code := browse(t, base, "/v1/agents/agt-two/workspace?at_turn=99"); code != 404 {
		t.Fatalf("at_turn=99: want 404, got %d", code)
	}
	if _, code := browse(t, base, "/v1/agents/agt-two/workspace?seq=99999"); code != 404 {
		t.Fatalf("seq=99999: want 404, got %d", code)
	}
}

// TestForkByTurnAndBySeqAgree: forking by ordinal and by that boundary's
// sequence produce children rooted at the same point in the parent's history.
func TestForkByTurnAndBySeqAgree(t *testing.T) {
	s, base := newServerTwoActivations(t)
	at, err := s.ResolveTurn("agt-two", 2)
	if err != nil {
		t.Fatal(err)
	}

	byTurn := forkVia(t, base, `{"at_turn":2}`)
	bySeq := forkVia(t, base, `{"seq":`+strconv.FormatUint(at.Seq, 10)+`}`)
	if byTurn.ParentTurn != 2 || bySeq.ParentTurn != 2 {
		t.Fatalf("parent_turn = %d and %d, want 2 for both", byTurn.ParentTurn, bySeq.ParentTurn)
	}
	if byTurn.LastCheckpointID != bySeq.LastCheckpointID {
		t.Fatalf("forks start from different checkpoints: %s vs %s", byTurn.LastCheckpointID, bySeq.LastCheckpointID)
	}

	// An address the parent never reached is not-found, not the nearest turn.
	resp := postJSON(t, base, "/v1/agents/agt-two/fork", `{"at_turn":99}`)
	if resp.StatusCode != 404 {
		t.Fatalf("fork at turn 99: want 404, got %d", resp.StatusCode)
	}
	// Ambiguous or missing addresses are rejected outright.
	if resp := postJSON(t, base, "/v1/agents/agt-two/fork", `{}`); resp.StatusCode != 400 {
		t.Fatalf("fork with no address: want 400, got %d", resp.StatusCode)
	}
	if resp := postJSON(t, base, "/v1/agents/agt-two/fork", `{"at_turn":2,"seq":3}`); resp.StatusCode != 400 {
		t.Fatalf("fork with two addresses: want 400, got %d", resp.StatusCode)
	}
}

func forkVia(t *testing.T, base, body string) model.Agent {
	t.Helper()
	resp := postJSON(t, base, "/v1/agents/agt-two/fork", body)
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("fork %s: %d %s", body, resp.StatusCode, b)
	}
	var child model.Agent
	if err := json.NewDecoder(resp.Body).Decode(&child); err != nil {
		t.Fatal(err)
	}
	return child
}

func readCheckpointFileVia(t *testing.T, base, ckpt, path string) string {
	t.Helper()
	resp := get(t, base, "/v1/agents/agt-two/checkpoints/"+ckpt+"/file?path="+path)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("read %s from %s: %d", path, ckpt, resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}
