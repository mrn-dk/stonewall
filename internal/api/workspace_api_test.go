package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrn-dk/stonewall/internal/model"
	"github.com/mrn-dk/stonewall/internal/node"
	"github.com/mrn-dk/stonewall/internal/runtime"
	"github.com/mrn-dk/stonewall/internal/store"
)

// newServerForBrowse sets up a store + node + API server with one completed
// agent that has a checkpoint and workspace files.
func newServerForBrowse(t *testing.T) (*Server, *store.Store, string) {
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
	srv := New(":0", s, n, s.Root())
	ts := httptest.NewServer(srv.srv.Handler)
	t.Cleanup(ts.Close)
	srv.addr = ts.URL
	// Create an agent and run it so it has a checkpoint.
	a := &model.Agent{
		ID: "agt-browse", Image: "img", Goal: "g", Model: "m",
		Grants:    model.Grants{FS: map[string]string{"/workspace": "rw"}, Cmd: []string{"rg"}},
		Isolation: model.IsolationDedicated, Checkpoint: model.CheckpointPerTurn, State: model.StatePending,
	}
	if err := s.CreateAgent(a); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Activate(context.Background(), "agt-browse"); err != nil {
		t.Fatal(err)
	}
	return srv, s, ts.URL
}

func TestWorkspaceBrowseDoesNotMutateLiveWorkspace(t *testing.T) {
	_, s, base := newServerForBrowse(t)
	a, _ := s.GetAgent("agt-browse")
	if a.LastCheckpointID == "" {
		t.Fatal("want a checkpoint")
	}
	liveBefore, _ := filepath.Glob(filepath.Join(s.WorkspacePath("agt-browse"), "*"))

	resp := get(t, base, "/v1/agents/agt-browse/workspace")
	if resp.StatusCode != 200 {
		t.Fatalf("browse: %d", resp.StatusCode)
	}
	var tree struct {
		Files []struct {
			Path  string `json:"path"`
			IsDir bool   `json:"is_dir"`
			Size  int64  `json:"size"`
		} `json:"files"`
	}
	json.NewDecoder(resp.Body).Decode(&tree)
	if len(tree.Files) == 0 {
		t.Fatal("expected files in checkpoint")
	}

	liveAfter, _ := filepath.Glob(filepath.Join(s.WorkspacePath("agt-browse"), "*"))
	if !sameStringSlice(liveBefore, liveAfter) {
		t.Fatalf("browse mutated live workspace: before=%v after=%v", liveBefore, liveAfter)
	}
}

func TestCheckpointFileContentsReassemble(t *testing.T) {
	_, s, base := newServerForBrowse(t)
	a, _ := s.GetAgent("agt-browse")
	liveData, err := os.ReadFile(filepath.Join(s.WorkspacePath("agt-browse"), "turn_1.txt"))
	if err != nil {
		t.Fatal(err)
	}
	ckpt := a.LastCheckpointID
	resp := get(t, base, "/v1/agents/agt-browse/checkpoints/"+ckpt+"/file?path=turn_1.txt")
	if resp.StatusCode != 200 {
		t.Fatalf("file read: %d", resp.StatusCode)
	}
	apiData, _ := io.ReadAll(resp.Body)
	if string(apiData) != string(liveData) {
		t.Fatalf("checkpoint file differs from live: got %q want %q", apiData, liveData)
	}
}

func TestBrowseCheckpointFileTree(t *testing.T) {
	_, _, base := newServerForBrowse(t)
	resp := get(t, base, "/v1/agents/agt-browse/workspace?at_turn=2")
	if resp.StatusCode != 200 {
		t.Fatalf("browse@2: %d", resp.StatusCode)
	}
	var tree struct {
		Turn  int `json:"turn"`
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	json.NewDecoder(resp.Body).Decode(&tree)
	if tree.Turn != 2 {
		t.Fatalf("want turn 2, got %d", tree.Turn)
	}
	found := false
	for _, f := range tree.Files {
		if f.Path == "turn_1.txt" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected turn_1.txt in the checkpoint tree")
	}
}

func TestNodeStatsNoSecrets(t *testing.T) {
	_, _, base := newServerForBrowse(t)
	resp := get(t, base, "/v1/node/stats")
	if resp.StatusCode != 200 {
		t.Fatalf("node stats: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var stats map[string]any
	if err := json.Unmarshal(body, &stats); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"cpu_usage_percent", "memory_bytes", "memory_total_bytes", "storage_bytes"} {
		if _, ok := stats[k]; !ok {
			t.Fatalf("node stats missing %q: %s", k, body)
		}
	}
	if strings.Contains(string(body), "token") || strings.Contains(string(body), "secret") {
		t.Fatalf("node stats may contain a secret: %s", body)
	}
}

func TestBrowseUnknownAgent(t *testing.T) {
	_, _, base := newServerForBrowse(t)
	resp := get(t, base, "/v1/agents/nope/workspace")
	if resp.StatusCode != 404 {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func sameStringSlice(a, b []string) bool {
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
