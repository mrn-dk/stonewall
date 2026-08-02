package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mrn-dk/stonewall/internal/model"
)

// ErrNotFound is returned for missing agents, activations, or checkpoints.
var ErrNotFound = errors.New("not found")

func marshalGrants(g model.Grants) (string, error) {
	b, err := json.Marshal(g)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalGrants(s string) (model.Grants, error) {
	var g model.Grants
	if s == "" {
		s = "{}"
	}
	if err := json.Unmarshal([]byte(s), &g); err != nil {
		return model.Grants{}, err
	}
	return g, nil
}

func marshalMeta(m map[string]string) (string, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalMeta(s string) (map[string]string, error) {
	var m map[string]string
	if s == "" {
		s = "{}"
	}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// CreateAgent persists a new agent. It does not resolve defaults; the caller
// (control plane) must set isolation/checkpoint defaults before calling.
func (s *Store) CreateAgent(a *model.Agent) error {
	if a.ID == "" {
		return fmt.Errorf("store: agent id required")
	}
	if a.Isolation == "" {
		a.Isolation = model.IsolationDedicated
	}
	if a.Checkpoint == "" {
		a.Checkpoint = model.CheckpointOnWrite
	}
	grants, err := marshalGrants(a.Grants)
	if err != nil {
		return err
	}
	meta, err := marshalMeta(a.Metadata)
	if err != nil {
		return err
	}
	nowt := now().UnixNano()
	a.CreatedAt = time.Unix(0, nowt).UTC()
	a.UpdatedAt = a.CreatedAt
	_, err = s.db.Exec(
		`INSERT INTO agents
		 (id,image,goal,model,grants,isolation,checkpoint,parent_id,parent_turn,
		  state,activation_count,last_turn,last_seq,last_checkpoint,metadata,
		  crash_count,quarantined,quarantined_until,created_at,updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Image, a.Goal, a.Model, grants, string(a.Isolation), string(a.Checkpoint),
		a.ParentID, a.ParentTurn, string(a.State), a.ActivationCount, a.LastTurn, 0,
		a.LastCheckpointID, meta, a.CrashCount, boolToInt(a.Quarantined),
		a.QuarantinedUntil.UnixNano(), a.CreatedAt.UnixNano(), a.UpdatedAt.UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("store: create agent: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// scanAgent hydrates an agent from a row.
func scanAgent(sc interface{ Scan(...any) error }) (*model.Agent, error) {
	a := &model.Agent{}
	var grants, meta string
	var isolation, checkpoint, state string
	var quarantined int
	var quarUntil, createdN, updatedN int64
	err := sc.Scan(
		&a.ID, &a.Image, &a.Goal, &a.Model, &grants, &isolation, &checkpoint,
		&a.ParentID, &a.ParentTurn, &state, &a.ActivationCount, &a.LastTurn,
		&a.LastCheckpointID, &meta, &a.CrashCount, &quarantined, &quarUntil,
		&createdN, &updatedN,
	)
	if err != nil {
		return nil, err
	}
	a.Isolation = model.Isolation(isolation)
	a.Checkpoint = model.CheckpointPolicy(checkpoint)
	a.State = model.AgentState(state)
	a.Quarantined = quarantined == 1
	if quarUntil > 0 {
		a.QuarantinedUntil = time.Unix(0, quarUntil).UTC()
	}
	a.CreatedAt = time.Unix(0, createdN).UTC()
	a.UpdatedAt = time.Unix(0, updatedN).UTC()
	a.Grants, err = unmarshalGrants(grants)
	if err != nil {
		return nil, err
	}
	a.Metadata, err = unmarshalMeta(meta)
	return a, err
}

const agentCols = `id,image,goal,model,grants,isolation,checkpoint,parent_id,parent_turn,
 state,activation_count,last_turn,last_checkpoint,metadata,crash_count,quarantined,
 quarantined_until,created_at,updated_at`

// GetAgent loads an agent by ID.
func (s *Store) GetAgent(id string) (*model.Agent, error) {
	row := s.db.QueryRow(`SELECT `+agentCols+` FROM agents WHERE id = ?`, id)
	a, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// ListAgentsFilter filters the agent list.
type ListAgentsFilter struct {
	State model.AgentState
	// Query is a case-insensitive substring matched against goal and image.
	// Empty means no text filter. Filtering happens here rather than in the
	// caller so a client can search the fleet without loading it.
	Query string
	// AfterID is the cursor for paging (exclusive). Empty means from the start.
	AfterID string
	Limit   int
}

// likeEscape escapes the LIKE wildcards so a query containing % or _ matches
// those characters literally rather than acting as a pattern.
func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ListAgents returns agents ordered by created_at, id for stable paging.
func (s *Store) ListAgents(f ListAgentsFilter) ([]*model.Agent, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 100
	}
	q := `SELECT ` + agentCols + ` FROM agents`
	args := []any{}
	where := []string{}
	if f.State != "" {
		where = append(where, `state = ?`)
		args = append(args, string(f.State))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		// Substring match, not FTS: on a single node this scans a table that
		// comfortably holds the fleet sizes this store targets. What matters is
		// that the predicate lives here, so a fleet backend can swap it for an
		// index or an FTS5 table without any caller noticing.
		pattern := "%" + likeEscape(strings.ToLower(q)) + "%"
		where = append(where, `(LOWER(goal) LIKE ? ESCAPE '\' OR LOWER(image) LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern)
	}
	if f.AfterID != "" {
		// Cursor by (created_at,id): use a subquery to fetch the cursor row's key.
		where = append(where, `(created_at,id) > (SELECT created_at,id FROM agents WHERE id = ?)`)
		args = append(args, f.AfterID)
	}
	if len(where) > 0 {
		q += " WHERE " + joinStrings(where, " AND ")
	}
	q += " ORDER BY created_at ASC, id ASC LIMIT ?"
	args = append(args, f.Limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func joinStrings(ss []string, sep string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}

// UpdateState transitions an agent's state, enforcing legal transitions.
func (s *Store) UpdateState(id string, to model.AgentState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.GetAgent(id)
	if err != nil {
		return err
	}
	if !model.CanTransition(a.State, to) {
		return fmt.Errorf("store: illegal transition %s -> %s for agent %s", a.State, to, id)
	}
	_, err = s.db.Exec(
		`UPDATE agents SET state = ?, updated_at = ? WHERE id = ?`,
		string(to), now().UnixNano(), id,
	)
	return err
}

// advanceAgent records the event index after an append. It advances only
// last_seq and last_turn; last_checkpoint is owned by SetLastCheckpoint so
// ordinary event appends never clobber it.
//
// last_turn counts turn boundaries: a boundary increments it, everything else
// leaves it alone. It is an increment rather than an assignment because an
// assignment is what let a woken agent's per-activation counter overwrite the
// agent's position in its own history.
func (s *Store) advanceAgent(id string, lastSeq uint64, turnCompleted bool) error {
	inc := 0
	if turnCompleted {
		inc = 1
	}
	_, err := s.db.Exec(
		`UPDATE agents SET last_seq = ?, last_turn = last_turn + ?, updated_at = ? WHERE id = ?`,
		lastSeq, inc, now().UnixNano(), id,
	)
	return err
}

// IncrementActivationCount starts a new activation: increments the count,
// transitions to running (if legal), and returns the new activation number.
func (s *Store) IncrementActivationCount(id string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.GetAgent(id)
	if err != nil {
		return 0, err
	}
	n := a.ActivationCount + 1
	_, err = s.db.Exec(
		`UPDATE agents SET activation_count = ?, state = ?, updated_at = ? WHERE id = ?`,
		n, string(model.StateRunning), now().UnixNano(), id,
	)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// SetCrashState records a crash and applies backoff quarantine. Returns the
// updated agent so the node can decide whether to retry.
func (s *Store) SetCrashState(id string) (*model.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.GetAgent(id)
	if err != nil {
		return nil, err
	}
	a.CrashCount++
	// Exponential backoff with a cap; quarantine duration doubles per crash.
	backoff := time.Duration(1<<min(a.CrashCount, 8)) * time.Second
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	quarUntil := now().Add(backoff)
	_, err = s.db.Exec(
		`UPDATE agents SET crash_count = ?, quarantined = 1, quarantined_until = ?,
		 state = ?, updated_at = ? WHERE id = ?`,
		a.CrashCount, quarUntil.UnixNano(), string(model.StateParked), now().UnixNano(), id,
	)
	if err != nil {
		return nil, err
	}
	a.Quarantined = true
	a.QuarantinedUntil = quarUntil.UTC()
	a.State = model.StateParked
	return a, nil
}

// ClearCrashState resets crash counters after a successful activation.
func (s *Store) ClearCrashState(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`UPDATE agents SET crash_count = 0, quarantined = 0, quarantined_until = 0 WHERE id = ?`,
		id,
	)
	return err
}

// Quarantine marks an agent quarantined indefinitely (node circuit breaker).
func (s *Store) Quarantine(id string, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`UPDATE agents SET quarantined = 1, quarantined_until = ?, state = ?, updated_at = ? WHERE id = ?`,
		// far-future timestamp = indefinite quarantine
		now().Add(365*24*time.Hour).UnixNano(), string(model.StateFailed), now().UnixNano(), id,
	)
	_ = reason
	return err
}

// DeleteAgent removes an agent and all associated events, checkpoints, and
// workspace volumes, idempotently.
func (s *Store) DeleteAgent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM agents WHERE id = ?`, id)
	if err != nil {
		return err
	}
	// Remove on-disk artifacts.
	for _, sub := range []string{"events", "workspaces"} {
		if err := os.RemoveAll(filepath.Join(s.root, sub, id)); err != nil && !errors.Is(err, ErrNotFound) {
			// non-fatal: best-effort cleanup
		}
	}
	return nil
}
