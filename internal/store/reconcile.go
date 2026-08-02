package store

import (
	"encoding/json"

	"github.com/mrn-dk/stonewall/internal/model"
)

// reconcileFromLogs rebuilds the SQLite index from the JSONL logs at startup.
// The logs are the canonical record and the index is a recoverable projection
// of them, which is already how last_seq is treated.
//
// It recounts last_turn (turn boundaries are counted, so a log written by an
// earlier version — whose events carry colliding per-activation turn numbers —
// still yields a clean ascending ordinal) and backfills each checkpoint's
// boundary seq by matching checkpoint events to the boundary they followed.
//
// The pass is proportional to the total number of events. That is acceptable at
// single-node scale; a fleet-durable store should do this per agent as a
// migration step rather than a scan at startup.
func (s *Store) reconcileFromLogs() error {
	rows, err := s.db.Query(`SELECT id FROM agents`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.reconcileAgent(id); err != nil {
			return err
		}
	}
	return nil
}

// reconcileAgent recounts one agent's index from its log.
func (s *Store) reconcileAgent(agentID string) error {
	events, err := s.ReadEvents(agentID, 0, 0)
	if err != nil || len(events) == 0 {
		return err
	}
	completed := 0
	var lastSeq, lastBoundary uint64
	// checkpoint id -> the turn boundary it was produced at. The first
	// occurrence wins: checkpoint ids are content digests, so an unchanged
	// workspace re-checkpointed later is the same row, and the earliest
	// boundary keeps it resolvable from that point in the log onwards.
	produced := map[string]TurnBoundary{}
	for i, e := range events {
		if e.Seq > lastSeq {
			lastSeq = e.Seq
		}
		if i == 0 && e.Kind == model.EventFork {
			completed = forkParentTurn(e.Payload)
		}
		switch e.Kind {
		case model.EventTurnBoundary:
			completed++
			lastBoundary = e.Seq
		case model.EventCheckpoint:
			id := checkpointIDOf(e.Payload)
			if id == "" {
				continue
			}
			if _, seen := produced[id]; !seen {
				produced[id] = TurnBoundary{Turn: completed, Seq: lastBoundary}
			}
		}
	}
	if _, err := s.db.Exec(
		`UPDATE agents SET last_seq = ?, last_turn = ? WHERE id = ?`,
		lastSeq, completed, agentID,
	); err != nil {
		return err
	}
	for id, at := range produced {
		// Only rows that predate the column are backfilled; a checkpoint
		// written with its boundary already knows better than this scan.
		if _, err := s.db.Exec(
			`UPDATE checkpoints SET boundary_seq = ?, turn = ?
			 WHERE id = ? AND agent_id = ? AND boundary_seq = 0`,
			at.Seq, at.Turn, id, agentID,
		); err != nil {
			return err
		}
	}
	return nil
}

// checkpointIDOf reads the checkpoint id out of an EventCheckpoint payload.
func checkpointIDOf(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var p struct {
		CheckpointID string `json:"checkpoint_id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return ""
	}
	return p.CheckpointID
}
