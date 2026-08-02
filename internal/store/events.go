package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/mrn-dk/stonewall/internal/model"
)

// eventLog is an open handle to one agent's append-only JSONL log file. It is
// the canonical, instance-durable record. Writes are fsync'd before the
// caller acts on the result (write-ahead, spec §2.6, §5.1).
type eventLog struct {
	path string
	mu   sync.Mutex
	f    *os.File
	last uint64 // last allocated sequence number
	turn int    // last turn recorded
}

// eventPath is the JSONL file for an agent.
func (s *Store) eventPath(agentID string) string {
	return filepath.Join(s.root, "events", agentID, "log.jsonl")
}

// openLog opens (or creates) the event log for an agent and reconciles its
// last sequence/turn from the file so sequence allocation is crash-safe.
func (s *Store) openLog(agentID string) (*eventLog, error) {
	s.logsMu.Lock()
	defer s.logsMu.Unlock()
	if s.logs == nil {
		return nil, fmt.Errorf("store: closed")
	}
	if l, ok := s.logs[agentID]; ok {
		return l, nil
	}
	path := s.eventPath(agentID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	l := &eventLog{path: path}
	if err := l.reconcile(); err != nil {
		return nil, err
	}
	s.logs[agentID] = l
	return l, nil
}

// reconcile reads the tail of the file to recover last/turn after a crash.
func (l *eventLog) reconcile() error {
	f, err := os.Open(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // no events yet
		}
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var maxSeq uint64
	var maxTurn int
	for {
		var e model.Event
		if err := dec.Decode(&e); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			// A torn final line (crash mid-write) is skipped: the file is not
			// the source of truth until fsync, and the partial entry was never
			// acknowledged.
			if errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return err
		}
		if e.Seq > maxSeq {
			maxSeq = e.Seq
		}
		if e.Turn > maxTurn {
			maxTurn = e.Turn
		}
	}
	l.last = maxSeq
	l.turn = maxTurn
	return nil
}

// close closes the underlying file handle.
func (l *eventLog) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		l.f.Close()
		l.f = nil
	}
}

// append writes one event to the JSONL file, fsyncs it (instance-durable), and
// updates the SQLite index. Sequence is allocated here, monotonic per agent.
func (s *Store) AppendEvent(agentID, activationID string, kind model.EventKind, turn int, idem string, payload any) (*model.Event, error) {
	l, err := s.openLog(agentID)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// Materialize payload.
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("store: marshal event payload: %w", err)
		}
		raw = b
	}
	l.last++
	e := &model.Event{
		Seq:            l.last,
		AgentID:        agentID,
		ActivationID:   activationID,
		Kind:           kind,
		OccurredAt:     now(),
		Turn:           turn,
		Durability:     model.DurabilityFleet, // single node: local fsync == fleet ack
		IdempotencyKey: idem,
		Payload:        raw,
	}
	if turn > l.turn {
		l.turn = turn
	}
	line, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	line = append(line, '\n')

	// Open the file in append mode (created if missing).
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("store: open event log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return nil, fmt.Errorf("store: write event: %w", err)
	}
	// Write-ahead: fsync the file before any result is acted upon.
	if err := f.Sync(); err != nil {
		return nil, fmt.Errorf("store: fsync event log: %w", err)
	}

	// Update the SQLite index so list/stream queries have last_seq/last_turn.
	if err := s.advanceAgent(agentID, l.last, l.turn); err != nil {
		// Index lag is recoverable from the JSONL on restart; the event is
		// already durable. Log but do not fail the append.
		_ = err
	}
	return e, nil
}

// AppendForkPointer writes the initial EventFork entry to a freshly forked
// agent's log. The parent pointer is the first entry; reading history then
// walks the parent chain.
func (s *Store) AppendForkPointer(childID, parentID string, parentTurn int) error {
	payload := map[string]any{
		"parent_id":   parentID,
		"parent_turn": parentTurn,
	}
	_, err := s.AppendEvent(childID, "", model.EventFork, 0, "", payload)
	return err
}

// ReadEvents reads events for an agent with sequence > afterSeq, in order.
// If the agent is a fork, the caller may walk the parent chain via the fork
// pointer (see WalkHistory). limit<=0 means unbounded.
func (s *Store) ReadEvents(agentID string, afterSeq uint64, limit int) ([]*model.Event, error) {
	path := s.eventPath(agentID)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var out []*model.Event
	for {
		var e model.Event
		if err := dec.Decode(&e); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, err
		}
		if e.Seq <= afterSeq {
			continue
		}
		out = append(out, &e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// LastSeq returns the last allocated sequence number for an agent.
func (s *Store) LastSeq(agentID string) (uint64, error) {
	l, err := s.openLog(agentID)
	if err != nil {
		return 0, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.last, nil
}

// ForkPointer returns the parent agent id and turn recorded in the agent's
// first event if it is a fork. Returns ("",0,false) otherwise.
func (s *Store) ForkPointer(agentID string) (parentID string, parentTurn int, ok bool, err error) {
	events, err := s.ReadEvents(agentID, 0, 1)
	if err != nil || len(events) == 0 {
		return "", 0, false, err
	}
	if events[0].Kind != model.EventFork {
		return "", 0, false, nil
	}
	var p struct {
		ParentID   string `json:"parent_id"`
		ParentTurn int    `json:"parent_turn"`
	}
	if err := json.Unmarshal(events[0].Payload, &p); err != nil {
		return "", 0, false, err
	}
	return p.ParentID, p.ParentTurn, true, nil
}

// WalkHistory returns the full ordered history of an agent, walking its fork
// parent chain. Events from ancestors are returned first (up to the fork
// turn), then this agent's own events. This is how the live/audit view and the
// transcript load see a fork as one continuous history (spec §6.1).
func (s *Store) WalkHistory(agentID string, afterSeq uint64) ([]*model.Event, error) {
	var collected []*model.Event
	visited := map[string]bool{}
	cur := agentID
	curAfter := afterSeq
	// Collect this agent's own tail first.
	own, err := s.ReadEvents(agentID, curAfter, 0)
	if err != nil {
		return nil, err
	}
	// Walk ancestors prepended.
	chain := [][]model.Event{}
	// We re-encode: build ancestor segments.
	for {
		if visited[cur] {
			break // defensive against cycles
		}
		visited[cur] = true
		parentID, parentTurn, isFork, err := s.ForkPointer(cur)
		if err != nil {
			return nil, err
		}
		if !isFork {
			break
		}
		// Read ancestor events up to and including parentTurn (skip the fork's
		// own start; ancestors' events are theirs).
		anc, err := s.ReadEvents(parentID, 0, 0)
		if err != nil {
			return nil, err
		}
		var seg []model.Event
		for _, e := range anc {
			if e.Kind == model.EventFork {
				continue // fork pointers are structural, not conversation
			}
			if e.Turn <= parentTurn {
				seg = append(seg, *e)
			}
		}
		chain = append(chain, seg)
		cur = parentID
	}
	// Assemble: ancestors oldest-first (deepest first), then own tail.
	for i := len(chain) - 1; i >= 0; i-- {
		for j := range chain[i] {
			collected = append(collected, &chain[i][j])
		}
	}
	for _, e := range own {
		if e.Kind == model.EventFork {
			continue
		}
		collected = append(collected, e)
	}
	return collected, nil
}

// SetLastCheckpoint records the checkpoint id produced at a turn in the agent
// index (used by resume: restore to the checkpoint referenced by the last turn).
func (s *Store) SetLastCheckpoint(agentID string, checkpointID string) error {
	_, err := s.db.Exec(
		`UPDATE agents SET last_checkpoint = ?, updated_at = ? WHERE id = ?`,
		checkpointID, now().UnixNano(), agentID,
	)
	return err
}
