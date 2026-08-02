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

// Append implements runtime.EventSink with checkpointing.
func (c *checkpointingSink) Append(agentID, activationID string, kind model.EventKind, turn int, idem string, payload any) (uint64, error) {
	ev, err := c.store.AppendEvent(agentID, activationID, kind, turn, idem, payload)
	if err != nil {
		return 0, err
	}
	switch kind {
	case model.EventWorkspaceMod:
		c.modifiedThisTurn = true
	case model.EventTurnBoundary:
		if c.shouldSnapshot(turn) {
			if err := c.snapshot(turn); err != nil {
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
// it to the previous one.
func (c *checkpointingSink) snapshot(turn int) error {
	cp, err := c.store.SnapshotWorkspace(c.agentID, turn, c.workspaceDir, c.parent)
	if err != nil {
		return err
	}
	if _, err := c.store.AppendEvent(c.agentID, c.activationID, model.EventCheckpoint, turn, "", map[string]any{
		"checkpoint_id": cp.ID,
		"turn":          turn,
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
