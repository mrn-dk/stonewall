// Package model defines the core domain types of the Stonewall control plane.
//
// These types describe agents, activations, the capability grants applied per
// instance, the event log, and the content-addressed checkpoint store. They are
// pure data: behaviour lives in the store, node, runtime, and API packages.
//
// The model follows the v0.2 architecture: isolation is provided by the WASM
// runtime (WASIX), not by Stonewall. An agent's state lives outside its
// instance as an event log plus a workspace volume, so instances are
// disposable. Stonewall grants capabilities (filesystem, network, commands)
// per instance and schedules Latigo across hosts.
package model

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Isolation mode selects how a Latigo instance is hosted on a node.
//
//   - shared:        many instances per runtime process (same-tenant only).
//   - dedicated:     one runtime process per agent.
//   - dedicated_vm:  dedicated, inside a per-tenant microVM/container.
type Isolation string

const (
	IsolationShared      Isolation = "shared"
	IsolationDedicated   Isolation = "dedicated"
	IsolationDedicatedVM Isolation = "dedicated_vm"
)

// Validate reports an error for an unknown isolation mode.
func (i Isolation) Validate() error {
	switch i {
	case IsolationShared, IsolationDedicated, IsolationDedicatedVM:
		return nil
	case "":
		// Empty defaults to dedicated at creation time; treated as valid here.
		return nil
	}
	return fmt.Errorf("unknown isolation mode %q", i)
}

// CheckpointPolicy controls when a workspace snapshot is taken at turn
// boundaries. Checkpoints are always incremental and content-addressed.
type CheckpointPolicy string

const (
	CheckpointNone     CheckpointPolicy = "none"     // volume only; node loss loses the workspace
	CheckpointInterval CheckpointPolicy = "interval" // every N turns or T seconds
	CheckpointOnWrite  CheckpointPolicy = "on_write" // only on turns that modified the workspace (default)
	CheckpointPerTurn  CheckpointPolicy = "per_turn" // every turn boundary
)

func (c CheckpointPolicy) Validate() error {
	switch c {
	case CheckpointNone, CheckpointInterval, CheckpointOnWrite, CheckpointPerTurn:
		return nil
	case "":
		return nil // default applied by creation path
	}
	return fmt.Errorf("unknown checkpoint policy %q", c)
}

// AgentState is the lifecycle state of an agent resource.
type AgentState string

const (
	StatePending   AgentState = "pending"
	StateRunning   AgentState = "running"
	StateParked    AgentState = "parked"
	StateCompleted AgentState = "completed"
	StateFailed    AgentState = "failed"
	StateCancelled AgentState = "cancelled"
)

func (s AgentState) Terminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateCancelled:
		return true
	}
	return false
}

// legalTransitions is the set of allowed (from -> to) state changes.
var legalTransitions = map[AgentState]map[AgentState]struct{}{
	StatePending: {StateRunning: {}, StateCancelled: {}},
	StateRunning: {StateParked: {}, StateCompleted: {}, StateFailed: {}, StateCancelled: {}},
	StateParked:  {StateRunning: {}, StateCancelled: {}},
}

// CanTransition reports whether from -> to is legal.
func CanTransition(from, to AgentState) bool {
	if from == to {
		return true
	}
	tos, ok := legalTransitions[from]
	if !ok {
		return false
	}
	_, ok = tos[to]
	return ok
}

// Grants are the runtime capabilities applied per instance. Anything not
// granted does not exist from inside the sandbox.
//
//   - FS:  preopened directories, path -> "ro"|"rw".
//   - Net: endpoint allow-list (hostnames / host:port / Mortise / control plane).
//   - Cmd: command allow-list (binaries available to the agent's shell).
type Grants struct {
	FS  map[string]string `json:"fs,omitempty"`
	Net []string          `json:"net,omitempty"`
	Cmd []string          `json:"cmd,omitempty"`
}

// Validate checks grants are well-formed. It does not check whether a granted
// command is safe — the allow-list is a blast-radius and on-task tool; the
// security boundary is the sandbox (see spec §2.4 stated limitation).
func (g Grants) Validate() error {
	for path, mode := range g.FS {
		if path == "" {
			return fmt.Errorf("fs grant has empty path")
		}
		switch mode {
		case "ro", "rw":
		default:
			return fmt.Errorf("fs grant %q has invalid mode %q (want ro|rw)", path, mode)
		}
	}
	for _, e := range g.Net {
		if e == "" {
			return fmt.Errorf("net grant has empty endpoint")
		}
	}
	for _, c := range g.Cmd {
		if c == "" {
			return fmt.Errorf("cmd grant has empty command")
		}
	}
	return nil
}

// Agent is the durable agent resource: an identity, configuration, and a
// lifecycle that survives process death. State lives outside any instance.
type Agent struct {
	ID               string            `json:"id"`
	Image            string            `json:"image"` // content-addressed image digest or ref
	Goal             string            `json:"goal,omitempty"`
	Model            string            `json:"model,omitempty"`
	Grants           Grants            `json:"grants"`
	Isolation        Isolation         `json:"isolation"`
	Checkpoint       CheckpointPolicy  `json:"checkpoint"`
	ParentID         string            `json:"parent_id,omitempty"`   // fork: parent agent id
	ParentTurn       int               `json:"parent_turn,omitempty"` // fork: turn in parent
	State            AgentState        `json:"state"`
	ActivationCount  int               `json:"activation_count"`
	LastTurn         int               `json:"last_turn"`
	LastCheckpointID string            `json:"last_checkpoint_id,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`

	// Crash policy (spec §4.4): per-agent crash counter with backoff and
	// quarantine. A node-level circuit breaker also drains repeatedly crashing
	// runtimes; the per-agent counter is tracked here.
	CrashCount       int       `json:"crash_count"`
	Quarantined      bool      `json:"quarantined"`
	QuarantinedUntil time.Time `json:"quarantined_until,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Activation is a single run of the agent loop. Numbering is gapless per agent.
type Activation struct {
	ID          string     `json:"id"`
	AgentID     string     `json:"agent_id"`
	Number      int        `json:"number"`
	ImageDigest string     `json:"image_digest"` // exact digest this activation ran
	Grants      Grants     `json:"grants"`       // resolved grants snapshot
	Isolation   Isolation  `json:"isolation"`
	Token       string     `json:"-"` // ingest auth token (never serialized to clients)
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	EndReason   string     `json:"end_reason,omitempty"`
}

// EndReason values distinguish why an activation ended.
const (
	EndCompleted = "completed"
	EndCancelled = "cancelled"
	EndCrashed   = "crashed"
	EndBudget    = "budget_exhausted" // turns/tokens/wall-clock
	EndFenced    = "fenced"           // lease taken by another runner
)

// DurabilityLevel marks how far an event has propagated (spec §5.1).
type DurabilityLevel string

const (
	DurabilityInstance DurabilityLevel = "instance" // fsync'd to local volume
	DurabilityFleet    DurabilityLevel = "fleet"    // acknowledged by central storage
)

// EventKind enumerates the operational + conversational event log entries
// (spec §2.6). The log is append-only JSONL.
type EventKind string

const (
	EventRunStart     EventKind = "run_start"
	EventRunEnd       EventKind = "run_end"
	EventTurnBoundary EventKind = "turn"
	EventToolIntent   EventKind = "tool_intent"
	EventToolResult   EventKind = "tool_result"
	EventLLMCall      EventKind = "llm_call"
	EventCheckpoint   EventKind = "checkpoint"
	EventEgress       EventKind = "egress"
	EventMessage      EventKind = "message"
	EventApproval     EventKind = "approval"
	EventFork         EventKind = "fork" // parent pointer, recorded first in a fork's log
	EventWorkspaceMod EventKind = "workspace_modified"
)

// Event is one entry in an agent's append-only event log. Sequence numbers are
// strictly increasing per agent with no gaps, spanning all activations.
type Event struct {
	Seq          uint64    `json:"seq"`
	AgentID      string    `json:"agent_id"`
	ActivationID string    `json:"activation_id,omitempty"`
	Kind         EventKind `json:"kind"`
	OccurredAt   time.Time `json:"occurred_at"`
	// Turn is the authoritative turn ordinal, assigned by the store at append
	// time as a count of the turn boundaries in the agent's history. It spans
	// all activations and never restarts, so it addresses one turn in the
	// agent's life.
	//
	// It is deliberately NOT the number the runtime counted for itself: a
	// runtime's counter is a per-activation budget, so a guest emitting its
	// turn 3 may well see the log record turn 9. The guest's number is retained
	// in the payload as `runtime_turn` and is informational only.
	Turn           int             `json:"turn,omitempty"`
	Durability     DurabilityLevel `json:"durability"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
}

// FileEntry describes one file in a checkpoint manifest: its mode and the
// ordered list of content-addressed chunk digests it is composed of.
type FileEntry struct {
	Mode   os.FileMode `json:"mode"`
	Size   int64       `json:"size"`
	Chunks []string    `json:"chunks"` // sha256 digest of each chunk
}

// Checkpoint is an incremental, content-addressed workspace snapshot. A
// checkpoint references shared chunks from its parent for unchanged files;
// only dirty blocks produce new chunks. Each checkpoint ID is recorded in the
// event log at the turn that produced it (EventCheckpoint), so "restore to
// turn N" is a lookup and transcript and workspace are consistent by
// construction (spec §5.2, §6.1).
type Checkpoint struct {
	ID        string               `json:"id"` // digest of the manifest
	AgentID   string               `json:"agent_id"`
	Turn      int                  `json:"turn"`
	ParentID  string               `json:"parent_id,omitempty"`
	Manifest  map[string]FileEntry `json:"manifest"`
	CreatedAt time.Time            `json:"created_at"`
}
