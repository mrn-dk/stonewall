package store

import (
	"github.com/mrn-dk/stonewall/internal/model"
)

// TurnBoundary is one turn in an agent's history, addressed two ways: by its
// ordinal (the operator-facing name — "turn 12") and by the sequence number of
// the event that closed it (the canonical address of a point in the log).
//
// Both name exactly one point, because the ordinal is a count of boundaries
// rather than a number a writer supplied.
type TurnBoundary struct {
	Turn int    `json:"turn"`
	Seq  uint64 `json:"seq"`
}

// TurnBoundaries returns every turn boundary in an agent's log, in order. For a
// fork the ordinals continue from the turn it branched at, matching the history
// the agent presents.
func (s *Store) TurnBoundaries(agentID string) ([]TurnBoundary, error) {
	events, err := s.ReadEvents(agentID, 0, 0)
	if err != nil {
		return nil, err
	}
	n := 0
	var out []TurnBoundary
	for i, e := range events {
		if i == 0 && e.Kind == model.EventFork {
			n = forkParentTurn(e.Payload)
		}
		if e.Kind != model.EventTurnBoundary {
			continue
		}
		n++
		out = append(out, TurnBoundary{Turn: n, Seq: e.Seq})
	}
	return out, nil
}

// ResolveTurn resolves at_turn=N to the Nth turn boundary in the agent's log,
// counted across all of its activations.
//
// A turn the agent has not reached is ErrNotFound, never the nearest match:
// handing back a different point in history than the caller asked for is the
// failure mode this addressing exists to remove.
func (s *Store) ResolveTurn(agentID string, turn int) (TurnBoundary, error) {
	if turn <= 0 {
		return TurnBoundary{}, ErrNotFound
	}
	boundaries, err := s.TurnBoundaries(agentID)
	if err != nil {
		return TurnBoundary{}, err
	}
	for _, b := range boundaries {
		if b.Turn == turn {
			return b, nil
		}
	}
	return TurnBoundary{}, ErrNotFound
}

// ResolveSeq resolves a sequence number to the turn boundary it addresses, for
// callers that already hold one. A sequence that is not a turn boundary is
// ErrNotFound: it does not name a point a fork or a browse can be taken at.
func (s *Store) ResolveSeq(agentID string, seq uint64) (TurnBoundary, error) {
	boundaries, err := s.TurnBoundaries(agentID)
	if err != nil {
		return TurnBoundary{}, err
	}
	for _, b := range boundaries {
		if b.Seq == seq {
			return b, nil
		}
	}
	return TurnBoundary{}, ErrNotFound
}

// LastTurnBoundary returns the most recent turn boundary in an agent's log, or
// ErrNotFound if it has completed no turns.
func (s *Store) LastTurnBoundary(agentID string) (TurnBoundary, error) {
	boundaries, err := s.TurnBoundaries(agentID)
	if err != nil {
		return TurnBoundary{}, err
	}
	if len(boundaries) == 0 {
		return TurnBoundary{}, ErrNotFound
	}
	return boundaries[len(boundaries)-1], nil
}
