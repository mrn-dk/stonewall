package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mrn-dk/stonewall/internal/model"
	"github.com/mrn-dk/stonewall/internal/node"
	"github.com/mrn-dk/stonewall/internal/runtime"
	"github.com/mrn-dk/stonewall/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store, *node.Node) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	t.Setenv("MOCK_TURNS", "2")
	n := node.New(s, &runtime.MockRuntime{}, "", "", node.Config{
		MaxConcurrent: 2, PollInterval: 10 * time.Millisecond,
		ActivationTimeout: 10 * time.Second, CrashThreshold: 3, NodeBreakerThreshold: 100,
	})
	srv := New(":0", s, n, s.Root(), nil)
	ts := httptest.NewServer(srv.srv.Handler)
	t.Cleanup(ts.Close)
	// Replace addr with the test server's for telemetry only.
	srv.addr = ts.URL
	return srv, s, n
}

func TestAPICreateRunStreamFork(t *testing.T) {
	srv, s, n := newTestServer(t)
	base := srv.addr

	// Create an agent.
	body := `{"image":"acme/agent-host:1.4","goal":"hello","model":"m","grants":{"fs":{"/workspace":"rw"},"cmd":["rg"]},"isolation":"dedicated","checkpoint":"per_turn"}`
	resp := postJSON(t, base, "/v1/agents", body)
	var ag model.Agent
	json.NewDecoder(resp.Body).Decode(&ag)
	if ag.ID == "" || ag.State != model.StatePending {
		t.Fatalf("create: %+v", ag)
	}

	// Send a message -> enqueues input and parks the pending agent (runnable).
	resp = postJSON(t, base, "/v1/agents/"+ag.ID+"/messages", `{"body":"go","kind":"user"}`)
	if resp.StatusCode != 202 {
		t.Fatalf("send: %d", resp.StatusCode)
	}

	// Run the scheduler briefly so it picks up the runnable agent.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go n.Run(ctx)
	// Wait until the agent is parked (post-activation) and has a checkpoint.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		a, _ := s.GetAgent(ag.ID)
		if a.State == model.StateParked && a.LastCheckpointID != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()

	a, _ := s.GetAgent(ag.ID)
	if a.State != model.StateParked {
		t.Fatalf("want parked after run, got %s", a.State)
	}
	if a.LastCheckpointID == "" {
		t.Fatal("want checkpoint after per_turn run")
	}

	// Stream events (replay from 0).
	resp = get(t, base, "/v1/agents/"+ag.ID+"/events?after=0")
	defer resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type=%s", resp.Header.Get("Content-Type"))
	}
	// Read a few SSE lines; expect at least a run_start and a turn.
	got := readSSE(t, resp.Body)
	if !strings.Contains(got, "run_start") || !strings.Contains(got, "turn") {
		t.Fatalf("stream missing events: %s", got)
	}

	// Fork at turn 1.
	resp = postJSON(t, base, "/v1/agents/"+ag.ID+"/fork", `{"at_turn":1}`)
	if resp.StatusCode != 201 {
		t.Fatalf("fork: %d", resp.StatusCode)
	}
	var child model.Agent
	json.NewDecoder(resp.Body).Decode(&child)
	if child.ParentID != ag.ID {
		t.Fatalf("fork parent = %s", child.ParentID)
	}
	// Child workspace inherited turn_1.txt.
	if _, err := os.ReadFile(s.WorkspacePath(child.ID) + "/turn_1.txt"); err != nil {
		t.Fatalf("fork workspace missing turn_1.txt: %v", err)
	}

	// Restore the parent to its checkpoint.
	resp = postJSON(t, base, "/v1/agents/"+ag.ID+"/restore", `{"checkpoint_id":"`+a.LastCheckpointID+`"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("restore: %d", resp.StatusCode)
	}

	// List agents.
	resp = get(t, base, "/v1/agents?limit=10")
	if resp.StatusCode != 200 {
		t.Fatalf("list: %d", resp.StatusCode)
	}

	// Get agent.
	resp = get(t, base, "/v1/agents/"+ag.ID)
	if resp.StatusCode != 200 {
		t.Fatalf("get: %d", resp.StatusCode)
	}

	// List activations.
	resp = get(t, base, "/v1/agents/"+ag.ID+"/activations")
	if resp.StatusCode != 200 {
		t.Fatalf("activations: %d", resp.StatusCode)
	}

	// Delete.
	resp = del(t, base, "/v1/agents/"+ag.ID)
	if resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
}

func TestAPIValidation(t *testing.T) {
	srv, _, _ := newTestServer(t)
	// Missing image.
	resp := postJSON(t, srv.addr, "/v1/agents", `{"goal":"x"}`)
	if resp.StatusCode != 400 {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
	// Bad isolation.
	resp = postJSON(t, srv.addr, "/v1/agents", `{"image":"x","isolation":"bogus"}`)
	if resp.StatusCode != 400 {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
	// Get missing.
	resp = get(t, srv.addr, "/v1/agents/nope")
	if resp.StatusCode != 404 {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

// The `q` parameter narrows the list over the wire, composes with `state` and
// `limit`, and is absent-tolerant: no `q` behaves exactly as before.
func TestAPIListAgentsTextQuery(t *testing.T) {
	srv, _, _ := newTestServer(t)
	base := srv.addr

	for _, body := range []string{
		`{"image":"acme/agent-host:1.4","goal":"build the search index"}`,
		`{"image":"acme/agent-host:1.4","goal":"summarise the repo"}`,
		`{"image":"other/indexer:2.0","goal":"unrelated work"}`,
	} {
		if resp := postJSON(t, base, "/v1/agents", body); resp.StatusCode != 201 {
			t.Fatalf("create: want 201, got %d", resp.StatusCode)
		} else {
			resp.Body.Close()
		}
	}

	listed := func(path string) []map[string]any {
		t.Helper()
		resp := get(t, base, path)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s: want 200, got %d", path, resp.StatusCode)
		}
		var out struct {
			Agents []map[string]any `json:"agents"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("%s: decode: %v", path, err)
		}
		return out.Agents
	}

	if got := listed("/v1/agents"); len(got) != 3 {
		t.Fatalf("no query: want 3 agents, got %d", len(got))
	}
	// Matches goal and image, case-insensitively.
	if got := listed("/v1/agents?q=INDEX"); len(got) != 2 {
		t.Fatalf("q=INDEX: want 2 agents, got %d", len(got))
	}
	if got := listed("/v1/agents?q=summarise"); len(got) != 1 {
		t.Fatalf("q=summarise: want 1 agent, got %d", len(got))
	}
	// Composes with limit; the cursor continues the filtered sequence.
	page := listed("/v1/agents?q=index&limit=1")
	if len(page) != 1 {
		t.Fatalf("q=index&limit=1: want 1 agent, got %d", len(page))
	}
	rest := listed("/v1/agents?q=index&limit=5&after=" + page[0]["id"].(string))
	if len(rest) != 1 {
		t.Fatalf("second page: want 1 agent, got %d", len(rest))
	}
	// A query matching nothing is an empty success, not an error.
	resp := get(t, base, "/v1/agents?q=nothing-matches-this")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("no match: want 200, got %d", resp.StatusCode)
	}
	if got := listed("/v1/agents?q=nothing-matches-this"); len(got) != 0 {
		t.Fatalf("no match: want 0 agents, got %d", len(got))
	}
}

// helpers

func postJSON(t *testing.T, base, path, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", base+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
func get(t *testing.T, base, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
func del(t *testing.T, base, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("DELETE", base+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readSSE(t *testing.T, r io.Reader) string {
	t.Helper()
	br := bufio.NewReader(r)
	var buf bytes.Buffer
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		line, err := br.ReadString('\n')
		if line != "" {
			buf.WriteString(line)
		}
		if err != nil {
			break
		}
		if strings.Contains(buf.String(), "done") || strings.Contains(buf.String(), "run_end") {
			break
		}
	}
	return buf.String()
}
