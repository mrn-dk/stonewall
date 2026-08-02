package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mrn-dk/stonewall/internal/model"
)

// StartActivation records a new activation with resolved grants and image
// digest. The token authenticates event ingestion for the instance.
func (s *Store) StartActivation(agentID string, number int, imageDigest string, grants model.Grants, isolation model.Isolation, token string) (*model.Activation, error) {
	a := &model.Activation{
		ID:          newID(),
		AgentID:     agentID,
		Number:      number,
		ImageDigest: imageDigest,
		Grants:      grants,
		Isolation:   isolation,
		Token:       token,
		StartedAt:   now(),
	}
	g, err := marshalGrants(grants)
	if err != nil {
		return nil, err
	}
	_, err = s.db.Exec(
		`INSERT INTO activations (id,agent_id,number,image_digest,grants,isolation,token,started_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		a.ID, a.AgentID, a.Number, a.ImageDigest, g, string(a.Isolation), a.Token, a.StartedAt.UnixNano(),
	)
	if err != nil {
		return nil, fmt.Errorf("store: start activation: %w", err)
	}
	return a, nil
}

// EndActivation records the end of an activation with a reason.
func (s *Store) EndActivation(id string, reason string) error {
	t := now()
	_, err := s.db.Exec(
		`UPDATE activations SET ended_at = ?, end_reason = ? WHERE id = ?`,
		t.UnixNano(), reason, id,
	)
	return err
}

// GetActivation loads one activation.
func (s *Store) GetActivation(id string) (*model.Activation, error) {
	row := s.db.QueryRow(
		`SELECT id,agent_id,number,image_digest,grants,isolation,token,started_at,ended_at,end_reason
		 FROM activations WHERE id = ?`, id,
	)
	return scanActivation(row)
}

// ListActivations returns activations for an agent ordered by number.
func (s *Store) ListActivations(agentID string) ([]*model.Activation, error) {
	rows, err := s.db.Query(
		`SELECT id,agent_id,number,image_digest,grants,isolation,token,started_at,ended_at,end_reason
		 FROM activations WHERE agent_id = ? ORDER BY number ASC`, agentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Activation
	for rows.Next() {
		a, err := scanActivation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanActivation(sc interface{ Scan(...any) error }) (*model.Activation, error) {
	a := &model.Activation{}
	var grants, isolation string
	var startedN int64
	var endedN sql.NullInt64
	err := sc.Scan(
		&a.ID, &a.AgentID, &a.Number, &a.ImageDigest, &grants, &isolation,
		&a.Token, &startedN, &endedN, &a.EndReason,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	a.Grants, err = unmarshalGrants(grants)
	if err != nil {
		return nil, err
	}
	a.Isolation = model.Isolation(isolation)
	a.StartedAt = time.Unix(0, startedN).UTC()
	if endedN.Valid {
		t := time.Unix(0, endedN.Int64).UTC()
		a.EndedAt = &t
	}
	return a, nil
}

// LatestActivation returns the most recent activation for an agent, or
// ErrNotFound.
func (s *Store) LatestActivation(agentID string) (*model.Activation, error) {
	row := s.db.QueryRow(
		`SELECT id,agent_id,number,image_digest,grants,isolation,token,started_at,ended_at,end_reason
		 FROM activations WHERE agent_id = ? ORDER BY number DESC LIMIT 1`, agentID,
	)
	return scanActivation(row)
}
