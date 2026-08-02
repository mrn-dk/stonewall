// Package node is the Stonewall node agent: per-host instance lifecycle,
// capability grants, event shipping, and crash policy (spec §4.2, §4.4).
//
// It does not implement a sandbox, a virtual filesystem, a shell, or a tool
// registry — the WASM runtime provides those. The node runs Latigo instances
// via the runtime abstraction, grants capabilities per instance, ships events
// to the durable store with write-ahead ordering, snapshots workspaces at turn
// boundaries per the agent's checkpoint policy, and enforces the crash policy:
// per-agent crash counters with backoff/quarantine plus a node-level circuit
// breaker that drains a repeatedly crashing runtime.
package node

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mrn-dk/stonewall/internal/model"
	"github.com/mrn-dk/stonewall/internal/runtime"
	"github.com/mrn-dk/stonewall/internal/store"
)

// Config tunes the node agent.
type Config struct {
	// MaxConcurrent is the number of simultaneous activations on this node.
	MaxConcurrent int
	// PollInterval is how often the scheduler scans for runnable agents.
	PollInterval time.Duration
	// CheckpointIntervalTurns is the N for the "interval" checkpoint policy.
	CheckpointIntervalTurns int
	// CrashThreshold is the per-agent crash count after which the agent is
	// quarantined as failed.
	CrashThreshold int
	// NodeBreakerThreshold is the number of consecutive activation crashes
	// (any agent) within NodeBreakerWindow that trips the node circuit breaker.
	NodeBreakerThreshold int
	// NodeBreakerWindow is the rolling window for the node circuit breaker.
	NodeBreakerWindow time.Duration
	// NodeBreakerCooldown is how long the node refuses new activations after
	// tripping.
	NodeBreakerCooldown time.Duration
	// ActivationTimeout bounds a single activation's wall-clock; 0 = unbounded.
	ActivationTimeout time.Duration
}

func (c *Config) defaults() {
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 4
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 500 * time.Millisecond
	}
	if c.CheckpointIntervalTurns <= 0 {
		c.CheckpointIntervalTurns = 5
	}
	if c.CrashThreshold <= 0 {
		c.CrashThreshold = 5
	}
	if c.NodeBreakerThreshold <= 0 {
		c.NodeBreakerThreshold = 10
	}
	if c.NodeBreakerWindow <= 0 {
		c.NodeBreakerWindow = 5 * time.Minute
	}
	if c.NodeBreakerCooldown <= 0 {
		c.NodeBreakerCooldown = 30 * time.Second
	}
}

// Node is the per-host node agent.
type Node struct {
	cfg   Config
	store *store.Store
	rt    runtime.Runtime
	tools string // image tools root (ro), optional

	// running tracks activations in flight for concurrency limiting.
	running int32
	sem     chan struct{}

	// nodeBreaker: rolling count of recent crashes and the tripped-until time.
	breakerMu    sync.Mutex
	breakerTimes []time.Time
	trippedUntil time.Time

	// cancel per-agent in-flight activation, for cancellation.
	actMu   sync.Mutex
	cancels map[string]context.CancelFunc

	// controlBase is the base URL Latigo posts events to (wasmer path).
	controlBase string
}

// New creates a node agent. controlBase is the ingest endpoint base URL for the
// wasmer path (may be empty for the mock).
func New(s *store.Store, rt runtime.Runtime, tools string, controlBase string, cfg Config) *Node {
	cfg.defaults()
	return &Node{
		cfg: cfg, store: s, rt: rt, tools: tools, controlBase: controlBase,
		sem:     make(chan struct{}, cfg.MaxConcurrent),
		cancels: map[string]context.CancelFunc{},
	}
}

// Scheduler loop: claims runnable agents and runs them, enforcing concurrency,
// quarantine, and the node circuit breaker. Blocks until ctx is cancelled.
func (n *Node) Run(ctx context.Context) error {
	t := time.NewTicker(n.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			n.scanOnce(ctx)
		}
	}
}

// scanOnce claims and runs runnable agents up to the concurrency limit.
func (n *Node) scanOnce(ctx context.Context) {
	if n.breakerTripped() {
		return
	}
	// List all non-terminal agents; run those that are runnable.
	agents, err := n.store.ListAgents(store.ListAgentsFilter{Limit: 200})
	if err != nil {
		return
	}
	for _, a := range agents {
		if a.State.Terminal() {
			continue
		}
		if !n.runnable(a) {
			continue
		}
		// Try to acquire a concurrency slot (non-blocking).
		select {
		case n.sem <- struct{}{}:
		default:
			return // at capacity
		}
		go func(a *model.Agent) {
			defer func() { <-n.sem }()
			n.runActivationFor(ctx, a)
		}(a)
	}
}

// runnable reports whether an agent should be activated now.
func (n *Node) runnable(a *model.Agent) bool {
	if a.Quarantined && time.Now().Before(a.QuarantinedUntil) {
		return false
	}
	switch a.State {
	case model.StatePending:
		return true
	case model.StateParked:
		has, err := n.store.HasPendingInputs(a.ID)
		if err != nil {
			return false
		}
		return has
	}
	return false
}

// runActivationFor runs one activation for an already-selected agent.
func (n *Node) runActivationFor(parent context.Context, a *model.Agent) {
	// Per-agent single-writer: skip if already running.
	n.actMu.Lock()
	if _, ok := n.cancels[a.ID]; ok {
		n.actMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	n.cancels[a.ID] = cancel
	n.actMu.Unlock()
	defer func() {
		n.actMu.Lock()
		delete(n.cancels, a.ID)
		n.actMu.Unlock()
	}()
	if n.cfg.ActivationTimeout > 0 {
		var tc context.CancelFunc
		ctx, tc = context.WithTimeout(ctx, n.cfg.ActivationTimeout)
		_ = tc
	}

	reason, err := n.RunActivation(ctx, a.ID)
	n.handleEnd(a.ID, reason, err)
}

// RunActivation runs a single activation for an agent: restores the workspace,
// starts the instance, and records the result. It is the resume path too — an
// agent is resumed by loading the transcript and restoring the workspace to the
// checkpoint referenced by the last turn (spec §5.3).
func (n *Node) RunActivation(ctx context.Context, agentID string) (string, error) {
	a, err := n.store.GetAgent(agentID)
	if err != nil {
		return model.EndCrashed, err
	}
	if a.State == model.StateRunning {
		return model.EndFenced, fmt.Errorf("agent %s already running", agentID)
	}
	// Start activation and consume queued inputs (exactly-once).
	number, err := n.store.IncrementActivationCount(agentID)
	if err != nil {
		return model.EndCrashed, err
	}
	inputs, err := n.store.ConsumeInputs(agentID)
	if err != nil {
		return model.EndCrashed, err
	}
	token := newToken()
	act, err := n.store.StartActivation(agentID, number, a.Image, a.Grants, a.Isolation, token)
	if err != nil {
		return model.EndCrashed, err
	}
	if err := n.store.UpdateState(agentID, model.StateRunning); err != nil {
		return model.EndCrashed, err
	}

	// Restore workspace from the last checkpoint (resume), or ensure empty.
	workspace, err := n.restoreOrInitWorkspace(a)
	if err != nil {
		n.store.EndActivation(act.ID, model.EndCrashed)
		return model.EndCrashed, err
	}

	// Resolve the parent checkpoint for incremental snapshots (the last one).
	parentCP := a.LastCheckpointID

	// Build the observing sink that snapshots at turn boundaries per policy.
	sink := &checkpointingSink{
		store:        n.store,
		agentID:      agentID,
		activationID: act.ID,
		workspaceDir: workspace,
		policy:       a.Checkpoint,
		interval:     n.cfg.CheckpointIntervalTurns,
		parent:       parentCP,
	}

	env := map[string]string{}
	if len(inputs) > 0 {
		env["LATIGO_INPUT"] = joinBodies(inputs)
	}

	spec := runtime.InstanceSpec{
		AgentID:         agentID,
		ActivationID:    act.ID,
		ImageDigest:     a.Image,
		WorkspaceDir:    workspace,
		ToolsDir:        n.tools,
		Goal:            a.Goal,
		Model:           a.Model,
		Grants:          grantsToMap(a.Grants),
		Isolation:       string(a.Isolation),
		Env:             env,
		ControlEndpoint: n.controlBase,
		EventSink:       sink,
	}
	// MaxTurns default; real budget comes from agent config. Keep a sane default.
	if spec.MaxTurns == 0 {
		spec.MaxTurns = 25
	}

	reason, rerr := n.rt.Run(ctx, spec)
	n.store.EndActivation(act.ID, reason)
	if rerr != nil && reason == "" {
		reason = model.EndCrashed
	}
	return reason, rerr
}

// restoreOrInitWorkspace restores the workspace from the agent's last
// checkpoint, or creates an empty one. For a fresh agent or fork it materializes
// the fork's starting checkpoint (set at fork time as LastCheckpointID).
func (n *Node) restoreOrInitWorkspace(a *model.Agent) (string, error) {
	if a.LastCheckpointID != "" {
		cp, err := n.store.GetCheckpoint(a.LastCheckpointID)
		if err == nil {
			if err := n.store.MaterializeWorkspace(a.ID, cp); err != nil {
				return "", err
			}
			return n.store.WorkspacePath(a.ID), nil
		}
		// checkpoint missing -> fall through to empty workspace
	}
	dir, err := n.store.EnsureWorkspace(a.ID)
	if err != nil {
		return "", err
	}
	return dir, nil
}

// handleEnd updates agent state and crash policy after an activation.
func (n *Node) handleEnd(agentID, reason string, err error) {
	switch reason {
	case model.EndCompleted, model.EndBudget:
		// Successful activation: park (idle, resumable) and clear crash state.
		_ = n.store.ClearCrashState(agentID)
		_ = n.store.UpdateState(agentID, model.StateParked)
	case model.EndFenced:
		_ = n.store.UpdateState(agentID, model.StateParked)
	case model.EndCancelled:
		_ = n.store.UpdateState(agentID, model.StateCancelled)
	case model.EndCrashed:
		n.recordCrash(agentID)
	default:
		_ = n.store.UpdateState(agentID, model.StateParked)
	}
}

// recordCrash applies per-agent backoff/quarantine and feeds the node breaker.
func (n *Node) recordCrash(agentID string) {
	a, err := n.store.SetCrashState(agentID)
	if err != nil || a == nil {
		return
	}
	if a.CrashCount >= n.cfg.CrashThreshold {
		_ = n.store.Quarantine(agentID, "crash threshold exceeded")
	}
	n.breakerHit()
}

// breakerHit records a crash for the node-level circuit breaker.
func (n *Node) breakerHit() {
	n.breakerMu.Lock()
	defer n.breakerMu.Unlock()
	now := time.Now()
	cutoff := now.Add(-n.cfg.NodeBreakerWindow)
	pruned := n.breakerTimes[:0]
	for _, t := range n.breakerTimes {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	pruned = append(pruned, now)
	n.breakerTimes = pruned
	if len(pruned) >= n.cfg.NodeBreakerThreshold {
		n.trippedUntil = now.Add(n.cfg.NodeBreakerCooldown)
	}
}

func (n *Node) breakerTripped() bool {
	n.breakerMu.Lock()
	defer n.breakerMu.Unlock()
	return time.Now().Before(n.trippedUntil)
}

// Activate runs one activation for an agent synchronously, applying the
// post-activation state/crash-policy handling. It is the single-activation path
// used by the scheduler (runActivationFor) and by tests; RunActivation alone
// does not apply end handling so that the scheduler controls it.
func (n *Node) Activate(ctx context.Context, agentID string) (string, error) {
	reason, err := n.RunActivation(ctx, agentID)
	n.handleEnd(agentID, reason, err)
	return reason, err
}

// Cancel cancels an in-flight activation, if any.
func (n *Node) Cancel(agentID string) error {
	n.actMu.Lock()
	c, ok := n.cancels[agentID]
	n.actMu.Unlock()
	if ok {
		c()
	}
	// Also mark the agent cancelled if not running (terminal cancel).
	a, err := n.store.GetAgent(agentID)
	if err != nil {
		return err
	}
	if !a.State.Terminal() && a.State != model.StateRunning {
		return n.store.UpdateState(agentID, model.StateCancelled)
	}
	return nil
}

// Fork creates a child agent from a parent at a turn boundary. It records the
// parent pointer as the first log entry and materializes the child's workspace
// as a copy-on-write view of the parent's checkpoint at that turn (spec §6.5).
func (n *Node) Fork(parentID string, atTurn int) (*model.Agent, error) {
	parent, err := n.store.GetAgent(parentID)
	if err != nil {
		return nil, err
	}
	// Find the parent's checkpoint at (or latest before) atTurn.
	cp, err := n.checkpointAtTurn(parentID, atTurn)
	if err != nil {
		return nil, fmt.Errorf("fork: no checkpoint to fork from at turn %d: %w", atTurn, err)
	}
	child := &model.Agent{
		ID:               newAgentID(),
		Image:            parent.Image,
		Goal:             parent.Goal,
		Model:            parent.Model,
		Grants:           parent.Grants,
		Isolation:        parent.Isolation,
		Checkpoint:       parent.Checkpoint,
		ParentID:         parentID,
		ParentTurn:       atTurn,
		State:            model.StateParked,
		LastCheckpointID: cp.ID,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}
	if err := n.store.CreateAgent(child); err != nil {
		return nil, err
	}
	// Log chaining: the fork records parent @ turn N as its first entry.
	if err := n.store.AppendForkPointer(child.ID, parentID, atTurn); err != nil {
		return nil, err
	}
	// Copy-on-write workspace: materialize from the parent checkpoint.
	if err := n.store.ForkWorkspace(child.ID, cp); err != nil {
		return nil, err
	}
	return child, nil
}

// checkpointAtTurn returns the parent's checkpoint at the requested turn
// boundary, or the nearest ancestor (spec §6.4: forks are valid only at turn
// boundaries).
func (n *Node) checkpointAtTurn(agentID string, atTurn int) (*model.Checkpoint, error) {
	return n.store.CheckpointForTurn(agentID, atTurn)
}

// grantsToMap converts model.Grants to the loose map the runtime expects.
func grantsToMap(g model.Grants) map[string]any {
	m := map[string]any{}
	fs := map[string]any{}
	for k, v := range g.FS {
		fs[k] = v
	}
	m["fs"] = fs
	m["net"] = append([]string(nil), g.Net...)
	m["cmd"] = append([]string(nil), g.Cmd...)
	return m
}

func joinBodies(in []*store.Input) string {
	out := ""
	for _, x := range in {
		if out != "" {
			out += "\n---\n"
		}
		out += x.Body
	}
	return out
}

func newAgentID() string { return "agt_" + randID() }
func newToken() string   { return "tok_" + randID() }

// randID is a short unique suffix.
func randID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// ErrNotImplemented is returned by unimplemented operations.
var ErrNotImplemented = errors.New("not implemented")
