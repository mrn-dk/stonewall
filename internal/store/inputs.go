package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mrn-dk/stonewall/internal/model"
)

// Input is one queued message for an agent (spec §4.5 "send message"). Delivery
// is exactly-once with respect to the durable record: consumption is marked in
// the same activation transaction.
type Input struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Body      string    `json:"body"`
	Kind      string    `json:"kind"` // "user" | "steer"
	CreatedAt time.Time `json:"created_at"`
	Consumed  bool      `json:"consumed"`
}

// EnqueueInput appends a message to an agent's input queue and makes the agent
// runnable (parked -> parked with pending work; the scheduler picks it up).
// Returns the new input. For a running agent, the input is delivered as
// in-run steering by the node (recorded as an EventMessage) rather than queued.
func (s *Store) EnqueueInput(agentID, body, kind string) (*Input, error) {
	if kind == "" {
		kind = "user"
	}
	in := &Input{
		ID:        uuid.NewString(),
		AgentID:   agentID,
		Body:      body,
		Kind:      kind,
		CreatedAt: now(),
	}
	_, err := s.db.Exec(
		`INSERT INTO agent_inputs (id,agent_id,body,kind,created_at) VALUES (?,?,?,?,?)`,
		in.ID, in.AgentID, in.Body, in.Kind, in.CreatedAt.UnixNano(),
	)
	if err != nil {
		return nil, fmt.Errorf("store: enqueue input: %w", err)
	}
	return in, nil
}

// PendingInputs returns unconsumed inputs for an agent, oldest first.
func (s *Store) PendingInputs(agentID string) ([]*Input, error) {
	rows, err := s.db.Query(
		`SELECT id,agent_id,body,kind,created_at,consumed FROM agent_inputs
		 WHERE agent_id = ? AND consumed = 0 ORDER BY created_at ASC`, agentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Input
	for rows.Next() {
		in, err := scanInput(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// HasPendingInputs reports whether the agent has any unconsumed inputs.
func (s *Store) HasPendingInputs(agentID string) (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM agent_inputs WHERE agent_id = ? AND consumed = 0`, agentID,
	).Scan(&n)
	return n > 0, err
}

// ConsumeInputs marks all pending inputs for an agent consumed and returns them
// (oldest first). Called at activation start with the write-ahead guarantee.
func (s *Store) ConsumeInputs(agentID string) ([]*Input, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(
		`SELECT id,agent_id,body,kind,created_at,consumed FROM agent_inputs
		 WHERE agent_id = ? AND consumed = 0 ORDER BY created_at ASC`, agentID,
	)
	if err != nil {
		return nil, err
	}
	var out []*Input
	for rows.Next() {
		in, err := scanInput(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, in)
	}
	rows.Close()
	if _, err := tx.Exec(
		`UPDATE agent_inputs SET consumed = 1, consumed_at = ? WHERE agent_id = ? AND consumed = 0`,
		now().UnixNano(), agentID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// steeringPayload for EventMessage.
type steeringPayload struct {
	Kind string `json:"kind"`
	Body string `json:"body"`
}

// MarkSteered records a steering message as an EventMessage in the log (for a
// running agent). This is the in-run steering path; it is recorded like any
// other hostcall so replay reconstructs it (spec §4.5).
func (s *Store) MarkSteered(agentID, activationID, body string) error {
	p := steeringPayload{Kind: "steer", Body: body}
	_, err := s.AppendEvent(agentID, activationID, model.EventMessage, 0, "", p)
	return err
}

func scanInput(sc interface{ Scan(...any) error }) (*Input, error) {
	in := &Input{}
	var consumed int
	var createdN int64
	if err := sc.Scan(&in.ID, &in.AgentID, &in.Body, &in.Kind, &createdN, &consumed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	in.CreatedAt = timeUnix(createdN)
	in.Consumed = consumed == 1
	return in, nil
}
