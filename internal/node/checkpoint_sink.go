package node

import (
	"github.com/mrn-dk/stonewall/internal/model"
	"github.com/mrn-dk/stonewall/internal/store"
)

// checkpointingSink wraps the durable store's event append and, at turn
// boundaries, snapshots the workspace per the agent's checkpoint policy. It is
// the write-ahead observation point: snapshots happen synchronously during the
// instance's event append, so the instance is blocked and the workspace is
// consistent (the instance fsyncs its writes before emitting the turn boundary).
//
// Each checkpoint is incremental: it references the previous checkpoint's
// chunks for unchanged files (spec §5.2). The checkpoint id is recorded as an
// EventCheckpoint so "restore to turn N" is a lookup (spec §5.2, §6.1).
type checkpointingSink struct {
	store        *store.Store
	agentID      string
	activationID string
	workspaceDir string
	policy       model.CheckpointPolicy
	interval     int    // turns, for policy=interval
	parent       string // parent checkpoint id (for incremental chaining)

	modifiedThisTurn bool
	lastSnapshotTurn int
}

// Append implements runtime.EventSink with checkpointing. The runtime's own
// turn number is passed through to the store as run-relative information; the
// turn a checkpoint is recorded at is the store-assigned ordinal on the
// appended event, which spans the agent's activations.
func (c *checkpointingSink) Append(agentID, activationID string, kind model.EventKind, runtimeTurn int, idem string, payload any) (uint64, error) {
	ev, err := c.store.AppendEvent(agentID, activationID, kind, runtimeTurn, idem, payload)
	if err != nil {
		return 0, err
	}
	switch kind {
	case model.EventWorkspaceMod:
		c.modifiedThisTurn = true
	case model.EventTurnBoundary:
		if c.shouldSnapshot(ev.Turn) {
			if err := c.snapshot(ev.Turn, ev.Seq); err != nil {
				// A checkpoint failure must not abort the activation; the event
				// is already durable. The workspace volume still holds the
				// state for instance-durable recovery.
				_ = err
			}
		}
		c.modifiedThisTurn = false
	}
	return ev.Seq, nil
}

// shouldSnapshot applies the checkpoint policy at a turn boundary.
func (c *checkpointingSink) shouldSnapshot(turn int) bool {
	switch c.policy {
	case model.CheckpointNone:
		return false
	case model.CheckpointPerTurn:
		return true
	case model.CheckpointOnWrite:
		return c.modifiedThisTurn
	case model.CheckpointInterval:
		if c.interval <= 1 {
			return true
		}
		return turn%c.interval == 0
	}
	return false
}

// snapshot creates an incremental checkpoint, records it in the log, and chains
// it to the previous one. boundarySeq addresses the turn boundary the workspace
// stood at, which is how the checkpoint is resolved later.
func (c *checkpointingSink) snapshot(turn int, boundarySeq uint64) error {
	cp, err := c.store.SnapshotWorkspace(c.agentID, turn, boundarySeq, c.workspaceDir, c.parent)
	if err != nil {
		return err
	}
	// The checkpoint event is appended after the boundary it belongs to, so its
	// own ordinal is the next turn's. The payload carries the boundary it
	// describes.
	if _, err := c.store.AppendEvent(c.agentID, c.activationID, model.EventCheckpoint, 0, "", map[string]any{
		"checkpoint_id": cp.ID,
		"turn":          turn,
		"boundary_seq":  boundarySeq,
		"parent":        cp.ParentID,
	}); err != nil {
		return err
	}
	if err := c.store.SetLastCheckpoint(c.agentID, cp.ID); err != nil {
		return err
	}
	c.parent = cp.ID
	c.lastSnapshotTurn = turn
	return nil
}
