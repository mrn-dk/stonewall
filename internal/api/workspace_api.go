package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/mrn-dk/stonewall/internal/model"
	"github.com/mrn-dk/stonewall/internal/store"
)

// nodeStatsHandler returns aggregate node resource usage for the dashboard's
// resource strip. Read-only, no agent data, no secrets.
func (s *Server) nodeStatsHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, collectNodeStats(s.dataDir))
}

// browseWorkspace returns a read-only file tree of an agent's workspace as it
// existed at a chosen turn (or the latest checkpoint when no address is given).
// It reconstructs from the content-addressed manifest and does NOT mutate the
// live workspace — distinct from the restore endpoint, which rewrites it.
//
//	Query: at_turn=<N>  the Nth turn boundary of the agent's log, across all
//	                    of its activations
//	Query: seq=<S>      that boundary's sequence number — the same address,
//	                    for callers that already hold one
//
// Both are optional and mutually exclusive; omitting them returns the most
// recently created checkpoint. A turn the agent has not reached is not-found.
func (s *Server) browseWorkspace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cp := s.resolveCheckpointForTurn(w, r, id)
	if cp == nil {
		return
	}
	nodes := s.store.BrowseCheckpoint(cp)
	writeJSON(w, 200, map[string]any{
		"agent_id":      id,
		"checkpoint_id": cp.ID,
		"turn":          cp.Turn,
		"boundary_seq":  cp.BoundarySeq,
		"files":         nodes,
	})
}

// browseCheckpointFiles returns the file tree for a specific checkpoint id.
func (s *Server) browseCheckpointFiles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ckpt := r.PathValue("ckpt")
	cp, err := s.store.GetCheckpoint(ckpt)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "checkpoint not found")
			return
		}
		internalErr(w, err.Error())
		return
	}
	if cp.AgentID != id {
		notFound(w, "checkpoint not found for agent")
		return
	}
	nodes := s.store.BrowseCheckpoint(cp)
	writeJSON(w, 200, map[string]any{
		"agent_id":      id,
		"checkpoint_id": cp.ID,
		"turn":          cp.Turn,
		"boundary_seq":  cp.BoundarySeq,
		"files":         nodes,
	})
}

// readCheckpointFile streams one file's contents reconstructed from a
// checkpoint's content-addressed chunks. Read-only; does not touch the live
// workspace. Capped rendering is the caller's responsibility (the dashboard).
//
//	Query: path=<relpath>  (required)
func (s *Server) readCheckpointFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ckpt := r.PathValue("ckpt")
	pathKey := r.URL.Query().Get("path")
	if pathKey == "" {
		badRequest(w, "path is required")
		return
	}
	cp, err := s.store.GetCheckpoint(ckpt)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "checkpoint not found")
			return
		}
		internalErr(w, err.Error())
		return
	}
	if cp.AgentID != id {
		notFound(w, "checkpoint not found for agent")
		return
	}
	entry, err := s.store.CheckpointFileEntry(cp, pathKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "file not in checkpoint")
			return
		}
		internalErr(w, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(entry.Size, 10))
	if err := s.store.ReadCheckpointFile(cp, pathKey, w); err != nil {
		// best-effort; headers may already be sent
		return
	}
}

// resolveCheckpointForTurn loads the workspace as of the point the request
// addresses — at_turn=N (the Nth turn boundary of the agent's log) or seq=S
// (that boundary's sequence) — or the most recently created checkpoint when
// neither is given. Writing an error response on failure; returns nil then.
func (s *Server) resolveCheckpointForTurn(w http.ResponseWriter, r *http.Request, agentID string) *model.Checkpoint {
	atTurn := 0
	if v := r.URL.Query().Get("at_turn"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			badRequest(w, "at_turn must be an integer")
			return nil
		}
		atTurn = n
	}
	var atSeq uint64
	if v := r.URL.Query().Get("seq"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			badRequest(w, "seq must be a non-negative integer")
			return nil
		}
		atSeq = n
	}
	if atTurn > 0 && atSeq > 0 {
		badRequest(w, "at_turn and seq address the same thing; supply only one")
		return nil
	}
	if atTurn > 0 || atSeq > 0 {
		at, err := s.resolveBoundary(agentID, atTurn, atSeq)
		if err == nil {
			var cp *model.Checkpoint
			cp, err = s.store.CheckpointAsOf(agentID, at.Seq)
			if err == nil {
				return cp
			}
		}
		if errors.Is(err, store.ErrNotFound) {
			if atSeq > 0 {
				notFound(w, "no turn boundary at seq "+strconv.FormatUint(atSeq, 10))
			} else {
				notFound(w, "no checkpoint at turn "+strconv.Itoa(atTurn))
			}
			return nil
		}
		internalErr(w, err.Error())
		return nil
	}
	cp, err := s.store.LatestCheckpointForAgent(agentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "agent has no checkpoints")
			return nil
		}
		internalErr(w, err.Error())
		return nil
	}
	return cp
}

// resolveBoundary resolves whichever address was supplied to the one turn
// boundary it names.
func (s *Server) resolveBoundary(agentID string, atTurn int, atSeq uint64) (store.TurnBoundary, error) {
	if atSeq > 0 {
		return s.store.ResolveSeq(agentID, atSeq)
	}
	return s.store.ResolveTurn(agentID, atTurn)
}
