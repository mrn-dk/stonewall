package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrn-dk/stonewall/internal/model"
)

func tmpStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustCreate(t *testing.T, s *Store, id string) *model.Agent {
	a := &model.Agent{
		ID:         id,
		Image:      "acme/agent-host:1.4",
		Goal:       "do the thing",
		Model:      "gpt-test",
		Grants:     model.Grants{FS: map[string]string{"/workspace": "rw"}, Net: []string{"mortise.internal"}, Cmd: []string{"rg"}},
		Isolation:  model.IsolationDedicated,
		Checkpoint: model.CheckpointOnWrite,
		State:      model.StatePending,
	}
	if err := s.CreateAgent(a); err != nil {
		t.Fatalf("create: %v", err)
	}
	return a
}

func TestCreateGetListAgent(t *testing.T) {
	s := tmpStore(t)
	a := mustCreate(t, s, "agt-1")
	got, err := s.GetAgent("agt-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Image != a.Image || got.Grants.Cmd[0] != "rg" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}

	// NotFound
	if _, err := s.GetAgent("missing"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	// List paging.
	mustCreate(t, s, "agt-2")
	mustCreate(t, s, "agt-3")
	all, err := s.ListAgents(ListAgentsFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 agents, got %d", len(all))
	}
	// State filter: pending -> running -> parked.
	if err := s.UpdateState("agt-2", model.StateRunning); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateState("agt-2", model.StateParked); err != nil {
		t.Fatal(err)
	}
	parked, _ := s.ListAgents(ListAgentsFilter{State: model.StateParked, Limit: 10})
	if len(parked) != 1 || parked[0].ID != "agt-2" {
		t.Fatalf("state filter: %+v", parked)
	}
}

func TestIllegalTransitionRejected(t *testing.T) {
	s := tmpStore(t)
	mustCreate(t, s, "a1")
	if err := s.UpdateState("a1", model.StateCompleted); err == nil {
		t.Fatal("pending->completed should be illegal")
	}
	if err := s.UpdateState("a1", model.StateRunning); err != nil {
		t.Fatalf("pending->running: %v", err)
	}
	if err := s.UpdateState("a1", model.StateParked); err != nil {
		t.Fatalf("running->parked: %v", err)
	}
}

func TestEventLogGaplessAndDurable(t *testing.T) {
	s := tmpStore(t)
	mustCreate(t, s, "a1")
	for i := 0; i < 5; i++ {
		_, err := s.AppendEvent("a1", "", model.EventTurnBoundary, i+1, "", map[string]any{"i": i})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	evs, err := s.ReadEvents("a1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 5 {
		t.Fatalf("want 5 events, got %d", len(evs))
	}
	for i, e := range evs {
		if e.Seq != uint64(i+1) {
			t.Fatalf("seq gap: got %d want %d", e.Seq, i+1)
		}
	}
	// The file is fsync'd JSONL.
	path := s.eventPath("a1")
	data, _ := os.ReadFile(path)
	if strings.Count(string(data), "\n") != 5 {
		t.Fatalf("want 5 lines, got %q", data)
	}
	var first model.Event
	if err := json.NewDecoder(strings.NewReader(string(data))).Decode(&first); err != nil {
		t.Fatal(err)
	}
	if first.Seq != 1 {
		t.Fatalf("first seq %d", first.Seq)
	}
}

func TestEventSeqReconciledAfterReopen(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	mustCreate(t, s, "a1")
	for i := 0; i < 3; i++ {
		s.AppendEvent("a1", "", model.EventTurnBoundary, i+1, "", nil)
	}
	s.Close()
	// Reopen; sequence must continue from the file, not restart.
	s2, _ := Open(dir)
	defer s2.Close()
	last, _ := s2.LastSeq("a1")
	if last != 3 {
		t.Fatalf("after reopen want last=3, got %d", last)
	}
	e, err := s2.AppendEvent("a1", "", model.EventTurnBoundary, 4, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.Seq != 4 {
		t.Fatalf("want seq 4 after reopen, got %d", e.Seq)
	}
}

func TestCheckpointIncrementalContentAddressed(t *testing.T) {
	s := tmpStore(t)
	mustCreate(t, s, "a1")
	ws, _ := s.EnsureWorkspace("a1")
	// Write a file.
	os.WriteFile(filepath.Join(ws, "a.txt"), []byte(strings.Repeat("A", 200_000)), 0o644)
	cp1, err := s.SnapshotWorkspace("a1", 1, 1, ws, "")
	if err != nil {
		t.Fatalf("snapshot1: %v", err)
	}
	// Modify only one file; add another. Unchanged file chunks are shared.
	os.WriteFile(filepath.Join(ws, "b.txt"), []byte("new"), 0o644)
	cp2, err := s.SnapshotWorkspace("a1", 2, 2, ws, cp1.ID)
	if err != nil {
		t.Fatalf("snapshot2: %v", err)
	}
	if cp1.ID == cp2.ID {
		t.Fatal("checkpoints must differ when content differs")
	}
	// a.txt chunks should be identical between manifests (incremental sharing).
	if !sameChunks(cp1.Manifest["a.txt"].Chunks, cp2.Manifest["a.txt"].Chunks) {
		t.Fatal("incremental: a.txt chunks should be shared with parent")
	}
	// Materialize cp2 into a fresh dir and verify content.
	if err := s.MaterializeWorkspace("a1", cp2); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(ws, "a.txt"))
	if len(got) != 200_000 {
		t.Fatalf("materialized a.txt len %d", len(got))
	}
	gotB, _ := os.ReadFile(filepath.Join(ws, "b.txt"))
	if string(gotB) != "new" {
		t.Fatalf("materialized b.txt = %q", gotB)
	}
}

func sameChunks(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestInputQueue(t *testing.T) {
	s := tmpStore(t)
	mustCreate(t, s, "a1")
	s.EnqueueInput("a1", "hello", "user")
	s.EnqueueInput("a1", "again", "user")
	pending, _ := s.PendingInputs("a1")
	if len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d", len(pending))
	}
	consumed, err := s.ConsumeInputs("a1")
	if err != nil || len(consumed) != 2 {
		t.Fatalf("consume: %v len=%d", err, len(consumed))
	}
	pending2, _ := s.PendingInputs("a1")
	if len(pending2) != 0 {
		t.Fatalf("want 0 pending after consume, got %d", len(pending2))
	}
}

func TestForkPointerAndHistoryWalk(t *testing.T) {
	s := tmpStore(t)
	// Parent: 3 turn events.
	mustCreate(t, s, "parent")
	for i := 0; i < 3; i++ {
		s.AppendEvent("parent", "", model.EventTurnBoundary, i+1, "", map[string]any{"turn": i + 1})
	}
	// Child fork: parent @ turn 2.
	mustCreate(t, s, "child")
	if err := s.AppendForkPointer("child", "parent", 2); err != nil {
		t.Fatal(err)
	}
	// Child's own events.
	s.AppendEvent("child", "", model.EventTurnBoundary, 3, "", map[string]any{"turn": 3})

	pid, pturn, ok, err := s.ForkPointer("child")
	if err != nil || !ok {
		t.Fatalf("fork pointer: ok=%v err=%v", ok, err)
	}
	if pid != "parent" || pturn != 2 {
		t.Fatalf("pointer = %s@%d", pid, pturn)
	}
	hist, err := s.WalkHistory("child", 0)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	// Expect: parent turns 1,2 (turn<=2) then child turn 3. Fork pointer excluded.
	turns := []int{}
	for _, e := range hist {
		if e.Kind == model.EventTurnBoundary {
			turns = append(turns, e.Turn)
		}
	}
	want := []int{1, 2, 3}
	if len(turns) != len(want) || turns[0] != 1 || turns[1] != 2 || turns[2] != 3 {
		t.Fatalf("history turns = %v want %v", turns, want)
	}
}

// mustCreateWith creates an agent with a specific goal and image so the text
// query has something meaningful to match.
func mustCreateWith(t *testing.T, s *Store, id, goal, image string) *model.Agent {
	t.Helper()
	a := &model.Agent{
		ID:         id,
		Image:      image,
		Goal:       goal,
		Model:      "gpt-test",
		Grants:     model.Grants{FS: map[string]string{"/workspace": "rw"}},
		Isolation:  model.IsolationDedicated,
		Checkpoint: model.CheckpointOnWrite,
		State:      model.StatePending,
	}
	if err := s.CreateAgent(a); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
	return a
}

func ids(agents []*model.Agent) []string {
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		out = append(out, a.ID)
	}
	return out
}

// The text query narrows the list server-side, matches goal and image
// case-insensitively, and treats LIKE metacharacters as literals.
func TestListAgentsTextQuery(t *testing.T) {
	s := tmpStore(t)
	mustCreateWith(t, s, "agt-1", "build the search index", "acme/agent-host:1.4")
	mustCreateWith(t, s, "agt-2", "summarise the repo", "acme/agent-host:1.4")
	mustCreateWith(t, s, "agt-3", "unrelated work", "other/INDEXER:2.0")
	mustCreateWith(t, s, "agt-4", "100% coverage", "acme/agent-host:1.4")

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"matches goal", "search", []string{"agt-1"}},
		{"matches image", "other/", []string{"agt-3"}},
		{"case-insensitive on both sides", "INDEX", []string{"agt-1", "agt-3"}},
		{"empty query does not filter", "", []string{"agt-1", "agt-2", "agt-3", "agt-4"}},
		{"whitespace-only query does not filter", "   ", []string{"agt-1", "agt-2", "agt-3", "agt-4"}},
		{"percent is a literal, not a wildcard", "100%", []string{"agt-4"}},
		{"underscore is a literal, not a wildcard", "100_", nil},
		{"no match returns nothing", "nothing-matches-this", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ListAgents(ListAgentsFilter{Query: tc.query, Limit: 10})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("query %q: want %v, got %v", tc.query, tc.want, ids(got))
			}
			for i, id := range tc.want {
				if got[i].ID != id {
					t.Fatalf("query %q: want %v, got %v", tc.query, tc.want, ids(got))
				}
			}
		})
	}
}

// The query composes with the state filter and with cursor paging: paging
// through a filtered list continues the same filtered sequence.
func TestListAgentsQueryComposesWithStateAndPaging(t *testing.T) {
	s := tmpStore(t)
	mustCreateWith(t, s, "agt-1", "index the corpus", "acme/agent-host:1.4")
	mustCreateWith(t, s, "agt-2", "index the docs", "acme/agent-host:1.4")
	mustCreateWith(t, s, "agt-3", "index the code", "acme/agent-host:1.4")
	mustCreateWith(t, s, "agt-4", "summarise the repo", "acme/agent-host:1.4")

	// agt-2 is the only running agent matching "index".
	if err := s.UpdateState("agt-2", model.StateRunning); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateState("agt-4", model.StateRunning); err != nil {
		t.Fatal(err)
	}
	running, err := s.ListAgents(ListAgentsFilter{State: model.StateRunning, Query: "index", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(running) != 1 || running[0].ID != "agt-2" {
		t.Fatalf("state+query: want [agt-2], got %v", ids(running))
	}

	// Paging: two pages of one, staying within the filtered sequence.
	first, err := s.ListAgents(ListAgentsFilter{Query: "index", Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(first) != 2 || first[0].ID != "agt-1" || first[1].ID != "agt-2" {
		t.Fatalf("first page: want [agt-1 agt-2], got %v", ids(first))
	}
	next, err := s.ListAgents(ListAgentsFilter{Query: "index", AfterID: first[1].ID, Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(next) != 1 || next[0].ID != "agt-3" {
		t.Fatalf("second page: want [agt-3], got %v", ids(next))
	}
}
