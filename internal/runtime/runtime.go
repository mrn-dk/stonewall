// Package runtime abstracts the WASM runtime that hosts Latigo instances.
//
// In the v0.2 architecture Latigo is an ordinary WASIX program; Stonewall does
// not implement a sandbox or a custom ABI. It runs an instance by invoking the
// runtime with the granted capabilities:
//
//	wasmer run latigo.wasm --dir /workspace --net ...
//
// Two implementations are provided:
//
//   - WasmerRuntime shells out to the `wasmer` CLI. It applies filesystem and
//     network grants as runtime flags and injects agent configuration (goal,
//     model, budgets, control-plane endpoint) via environment variables. This
//     is the real deployment path, exercised when a WASIX image and the wasmer
//     binary are present.
//
//   - MockRuntime is an in-process simulation of the agent loop. It writes a
//     realistic event-log sequence through the EventSink and produces workspace
//     checkpoints, so the whole control plane is testable end-to-end without
//     wasmer or a compiled guest. It stands in for the harness exactly where the
//     WASM boundary would be.
//
// Both push events through the same EventSink, which the node agent backs with
// the durable store (write-ahead, fsync). For the wasmer path, the EventSink is
// bridged from a localhost ingest server that Latigo posts to over the network.
package runtime

import (
	"context"

	"github.com/mrn-dk/stonewall/internal/model"
)

// InstanceSpec describes one Latigo activation to run.
type InstanceSpec struct {
	AgentID      string
	ActivationID string
	ImageDigest  string // exact image digest this activation runs
	WorkspaceDir string // mounted /workspace (rw)
	ToolsDir     string // mounted /tools (ro), from the image
	Goal         string
	Model        string
	MaxTurns     int
	Grants       map[string]any    // fs/net/cmd as resolved by the node
	Isolation    string            // shared|dedicated|dedicated_vm
	Env          map[string]string // extra env vars (e.g. LATIGO_*)
	// ControlEndpoint is the base URL Latigo posts events to (wasmer path). For
	// the mock it is unused; the mock calls EventSink directly.
	ControlEndpoint string
	// EventSink receives the agent's events with write-ahead durability. For
	// the wasmer path this is wired to the node's ingest server; for the mock
	// it is called in-process.
	EventSink EventSink
}

// EventSink is the bridge from a running instance to the durable log. It is the
// write-ahead surface: an event must be durable (fsync'd) before any result it
// describes is acted upon. Implementations live in the store package.
type EventSink interface {
	// Append records one event and returns its assigned sequence number.
	Append(agentID, activationID string, kind model.EventKind, turn int, idem string, payload any) (seq uint64, err error)
}

// Runtime hosts Latigo instances.
type Runtime interface {
	// Run executes one activation to completion (or context cancellation). It
	// returns the terminal end reason (see model.EndReason values). Run must be
	// safe to call concurrently across agents for distinct specs.
	Run(ctx context.Context, spec InstanceSpec) (endReason string, err error)

	// Name identifies the runtime for telemetry and selection.
	Name() string
}
