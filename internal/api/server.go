// Package api is the Stonewall control-plane HTTP surface (spec §4.5).
//
// Endpoints (all under /v1):
//
//	POST   /v1/agents                      create an agent
//	GET    /v1/agents                      list agents (cursor paging, state filter)
//	GET    /v1/agents/{id}                 get an agent
//	DELETE /v1/agents/{id}                 destroy an agent
//	POST   /v1/agents/{id}/messages       send a message (wake / in-run steer)
//	POST   /v1/agents/{id}/cancel          cancel an agent
//	POST   /v1/agents/{id}/checkpoint       take an explicit checkpoint
//	POST   /v1/agents/{id}/fork             fork at a turn boundary
//	POST   /v1/agents/{id}/restore          restore workspace to a checkpoint
//	POST   /v1/agents/{id}/approvals/{aid}  resolve an approval
//	GET    /v1/agents/{id}/events          stream events (SSE, resume via Last-Event-ID)
//	GET    /v1/agents/{id}/activations     list activations
//	GET    /healthz  /readyz               liveness / readiness
//
// The stream returned to clients is the durable log, so live view and audit
// record are the same object and reconnect is "stream from event N".
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mrn-dk/stonewall/internal/model"
	"github.com/mrn-dk/stonewall/internal/node"
	"github.com/mrn-dk/stonewall/internal/store"
)

// Server is the control-plane HTTP server.
type Server struct {
	store   *store.Store
	node    *node.Node
	addr    string
	dataDir string
	srv     *http.Server
}

// New creates a control-plane server bound to addr. dataDir is the store's
// root, used only for node storage-size stats (read through the API).
// dashFS is the embedded dashboard filesystem (nil when not built into this
// binary; the /dashboard handler then returns 503).
func New(addr string, s *store.Store, n *node.Node, dataDir string, dashFS fs.FS) *Server {
	srv := &Server{store: s, node: n, addr: addr, dataDir: dataDir}
	mux := http.NewServeMux()
	srv.register(mux)
	handler := requestLogger(srv.routeDashboard(mux, dashFS))
	srv.srv = &http.Server{Addr: addr, Handler: handler}
	return srv
}

// routeDashboard wraps the API mux so API paths hit the mux and everything
// else ("/", "/dashboard", "/_app/...", and client-side SPA routes like
// "/agents/:id") is served by the embedded SPA with index.html fallback. This
// avoids registering a catch-all "/" pattern that would conflict with the
// API's method patterns.
func (s *Server) routeDashboard(api http.Handler, dashFS fs.FS) http.Handler {
	dash := serveDashboardHandler(dashFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/v1/") || p == "/healthz" || p == "/readyz" {
			api.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(p, "/dashboard") {
			http.StripPrefix("/dashboard", dash).ServeHTTP(w, r)
			return
		}
		dash.ServeHTTP(w, r)
	})
}

func (s *Server) register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/readyz", s.readyz)
	mux.HandleFunc("POST /v1/agents", s.createAgent)
	mux.HandleFunc("GET /v1/agents", s.listAgents)
	mux.HandleFunc("GET /v1/agents/{id}", s.getAgent)
	mux.HandleFunc("DELETE /v1/agents/{id}", s.deleteAgent)
	mux.HandleFunc("POST /v1/agents/{id}/messages", s.sendMessage)
	mux.HandleFunc("POST /v1/agents/{id}/cancel", s.cancelAgent)
	mux.HandleFunc("POST /v1/agents/{id}/checkpoint", s.checkpoint)
	mux.HandleFunc("POST /v1/agents/{id}/fork", s.fork)
	mux.HandleFunc("POST /v1/agents/{id}/restore", s.restore)
	mux.HandleFunc("POST /v1/agents/{id}/approvals/{aid}", s.resolveApproval)
	mux.HandleFunc("GET /v1/agents/{id}/events", s.streamEvents)
	mux.HandleFunc("GET /v1/agents/{id}/activations", s.listActivations)
	mux.HandleFunc("GET /v1/node/stats", s.nodeStatsHandler)
	mux.HandleFunc("GET /v1/agents/{id}/workspace", s.browseWorkspace)
	mux.HandleFunc("GET /v1/agents/{id}/checkpoints/{ckpt}/files", s.browseCheckpointFiles)
	mux.HandleFunc("GET /v1/agents/{id}/checkpoints/{ckpt}/file", s.readCheckpointFile)
}

// ListenAndServe starts the control plane.
func (s *Server) ListenAndServe() error {
	log.Printf("stonewall control plane listening on %s", s.addr)
	return s.srv.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// ----- helpers -----

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorResponse{Code: code, Message: msg})
}

func badRequest(w http.ResponseWriter, msg string)  { writeErr(w, 400, "invalid", msg) }
func notFound(w http.ResponseWriter, msg string)    { writeErr(w, 404, "not_found", msg) }
func conflict(w http.ResponseWriter, msg string)    { writeErr(w, 409, "conflict", msg) }
func internalErr(w http.ResponseWriter, msg string) { writeErr(w, 500, "internal", msg) }

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func requestLogger(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func parseUint(v string, def uint64) uint64 {
	if v == "" {
		return def
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

// ----- handlers -----

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	// Readiness reflects store reachability.
	if _, err := s.store.ListAgents(store.ListAgentsFilter{Limit: 1}); err != nil {
		writeErr(w, 503, "not_ready", err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}

type createAgentRequest struct {
	Image      string                 `json:"image"`
	Goal       string                 `json:"goal"`
	Model      string                 `json:"model"`
	Grants     model.Grants           `json:"grants"`
	Isolation  model.Isolation        `json:"isolation"`
	Checkpoint model.CheckpointPolicy `json:"checkpoint"`
	Metadata   map[string]string      `json:"metadata"`
	MaxTurns   int                    `json:"max_turns"`
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	var req createAgentRequest
	if err := decode(r, &req); err != nil {
		badRequest(w, "invalid JSON: "+err.Error())
		return
	}
	if req.Image == "" {
		badRequest(w, "image is required")
		return
	}
	if req.Isolation == "" {
		req.Isolation = model.IsolationDedicated
	}
	if req.Checkpoint == "" {
		req.Checkpoint = model.CheckpointOnWrite
	}
	if err := req.Isolation.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Checkpoint.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := req.Grants.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	// shared is same-tenant only (spec §4.3). On a single node we accept it
	// but record the choice; cross-tenant enforcement is a fleet concern.
	a := &model.Agent{
		ID:         newAgentID(),
		Image:      req.Image,
		Goal:       req.Goal,
		Model:      req.Model,
		Grants:     req.Grants,
		Isolation:  req.Isolation,
		Checkpoint: req.Checkpoint,
		Metadata:   req.Metadata,
		State:      model.StatePending,
	}
	if err := s.store.CreateAgent(a); err != nil {
		internalErr(w, err.Error())
		return
	}
	writeJSON(w, 201, a)
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.ListAgentsFilter{
		State:   model.AgentState(q.Get("state")),
		AfterID: q.Get("after"),
		Limit:   int(parseUint(q.Get("limit"), 100)),
	}
	agents, err := s.store.ListAgents(f)
	if err != nil {
		internalErr(w, err.Error())
		return
	}
	next := ""
	if len(agents) > 0 {
		next = agents[len(agents)-1].ID
	}
	writeJSON(w, 200, map[string]any{"agents": agents, "next_cursor": next})
}

func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.store.GetAgent(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "agent not found")
			return
		}
		internalErr(w, err.Error())
		return
	}
	writeJSON(w, 200, a)
}

func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteAgent(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "agent not found")
			return
		}
		internalErr(w, err.Error())
		return
	}
	w.WriteHeader(204)
}

type sendMessageRequest struct {
	Body string `json:"body"`
	Kind string `json:"kind"` // user | steer; default user
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req sendMessageRequest
	if err := decode(r, &req); err != nil {
		badRequest(w, "invalid JSON: "+err.Error())
		return
	}
	a, err := s.store.GetAgent(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "agent not found")
			return
		}
		internalErr(w, err.Error())
		return
	}
	if a.State == model.StateRunning {
		// In-run steering: record as EventMessage. (The mock does not consume
		// live steering, but it is recorded durably as the spec requires.)
		act, _ := s.store.LatestActivation(id)
		aid := ""
		if act != nil {
			aid = act.ID
		}
		if err := s.store.MarkSteered(id, aid, req.Body); err != nil {
			internalErr(w, err.Error())
			return
		}
		writeJSON(w, 202, map[string]any{"delivered": "steered"})
		return
	}
	if a.State.Terminal() {
		conflict(w, "agent is terminal: "+string(a.State))
		return
	}
	in, err := s.store.EnqueueInput(id, req.Body, req.Kind)
	if err != nil {
		internalErr(w, err.Error())
		return
	}
	// A pending agent with a first input becomes runnable directly; a parked
	// agent is woken by the scheduler on its next scan.
	if a.State == model.StatePending {
		_ = s.store.UpdateState(id, model.StateParked)
	}
	writeJSON(w, 202, in)
}

func (s *Server) cancelAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.node.Cancel(id); err != nil {
		internalErr(w, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"state": "cancelled"})
}

func (s *Server) checkpoint(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.store.GetAgent(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "agent not found")
			return
		}
		internalErr(w, err.Error())
		return
	}
	ws := s.store.WorkspacePath(id)
	cp, err := s.store.SnapshotWorkspace(id, a.LastTurn, ws, a.LastCheckpointID)
	if err != nil {
		internalErr(w, err.Error())
		return
	}
	if _, err := s.store.AppendEvent(id, "", model.EventCheckpoint, a.LastTurn, "", map[string]any{
		"checkpoint_id": cp.ID, "turn": a.LastTurn, "parent": cp.ParentID, "explicit": true,
	}); err != nil {
		internalErr(w, err.Error())
		return
	}
	_ = s.store.SetLastCheckpoint(id, cp.ID)
	writeJSON(w, 201, cp)
}

type forkRequest struct {
	AtTurn int `json:"at_turn"`
}

func (s *Server) fork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req forkRequest
	if err := decode(r, &req); err != nil {
		badRequest(w, "invalid JSON: "+err.Error())
		return
	}
	if req.AtTurn < 0 {
		badRequest(w, "at_turn must be >= 0")
		return
	}
	child, err := s.node.Fork(id, req.AtTurn)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "parent agent not found")
			return
		}
		internalErr(w, err.Error())
		return
	}
	writeJSON(w, 201, child)
}

type restoreRequest struct {
	CheckpointID string `json:"checkpoint_id"`
}

func (s *Server) restore(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req restoreRequest
	if err := decode(r, &req); err != nil {
		badRequest(w, "invalid JSON: "+err.Error())
		return
	}
	if req.CheckpointID == "" {
		badRequest(w, "checkpoint_id is required")
		return
	}
	cp, err := s.store.GetCheckpoint(req.CheckpointID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "checkpoint not found")
			return
		}
		internalErr(w, err.Error())
		return
	}
	if err := s.store.MaterializeWorkspace(id, cp); err != nil {
		internalErr(w, err.Error())
		return
	}
	if err := s.store.SetLastCheckpoint(id, cp.ID); err != nil {
		internalErr(w, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"restored_to": cp.ID, "turn": strconv.Itoa(cp.Turn)})
}

type approvalRequest struct {
	Decision string `json:"decision"` // approved | denied
	Reason   string `json:"reason"`
}

func (s *Server) resolveApproval(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	aid := r.PathValue("aid")
	var req approvalRequest
	if err := decode(r, &req); err != nil {
		badRequest(w, "invalid JSON: "+err.Error())
		return
	}
	if req.Decision != "approved" && req.Decision != "denied" {
		badRequest(w, "decision must be approved|denied")
		return
	}
	_, err := s.store.AppendEvent(id, "", model.EventApproval, 0, "", map[string]any{
		"approval_id": aid, "decision": req.Decision, "reason": req.Reason,
	})
	if err != nil {
		internalErr(w, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"approval_id": aid, "decision": req.Decision})
}

func (s *Server) listActivations(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	acts, err := s.store.ListActivations(id)
	if err != nil {
		internalErr(w, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"activations": acts})
}

func newAgentID() string {
	return "agt_" + strconv.FormatInt(time.Now().UnixNano(), 36) + randSuffix()
}

func randSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()%100000)
}
