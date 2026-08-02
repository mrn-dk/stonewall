// Package store is Stonewall's durable storage for a single node.
//
// It owns three things:
//
//   - Agent/activation metadata and crash-policy counters in SQLite.
//   - The append-only JSONL event log, one file per agent, fsync'd before any
//     result is acted upon (write-ahead). The JSONL file is the canonical log;
//     SQLite holds a recoverable index (last_seq, last_turn) reconciled from
//     the file on startup.
//   - Content-addressed, incremental workspace checkpoints and copy-on-write
//     workspace volumes, materialized from chunk blobs.
//
// Single-node scope (spec build order step 3): one process, SQLite + local
// volume. The Store interface is written so a fleet-durable backend (central
// storage) can replace the local implementations later without changing
// callers.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	_ "modernc.org/sqlite"
)

// Store is the single-node durable store.
type Store struct {
	db   *sql.DB
	root string // data root: events/, chunks/, workspaces/

	mu     sync.Mutex           // serializes metadata writes per process
	logs   map[string]*eventLog // agentID -> open log handle (lazy)
	logsMu sync.Mutex

	// eventSeqMu serializes per-agent sequence allocation within this process.
	eventSeqMu sync.Mutex
}

// Open opens (or creates) a store rooted at dir. The SQLite database file and
// the event/checkpoint/workspace directories are created under it.
func Open(dir string) (*Store, error) {
	for _, sub := range []string{"", "events", "chunks", "workspaces"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("store: mkdir %s: %w", sub, err)
		}
	}
	dbPath := filepath.Join(dir, "stonewall.db")
	dsn := "file:" + dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping db: %w", err)
	}
	s := &Store{db: db, root: dir, logs: map[string]*eventLog{}}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database and any open log handles.
func (s *Store) Close() error {
	s.logsMu.Lock()
	for _, l := range s.logs {
		l.close()
	}
	s.logs = nil
	s.logsMu.Unlock()
	return s.db.Close()
}

// Root returns the store's data directory (used for storage-size stats).
func (s *Store) Root() string { return s.root }

func (s *Store) migrate() error {
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	// Additive column for stores created before checkpoints were addressed by
	// the turn boundary that produced them. Existing rows keep 0 until the
	// startup reconciliation backfills them from the log.
	if err := s.addColumn("checkpoints", "boundary_seq", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("store: migrate checkpoints.boundary_seq: %w", err)
	}
	return nil
}

// addColumn adds a column if the table does not already have it.
func (s *Store) addColumn(table, column, decl string) error {
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + decl)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS agents (
    id               TEXT PRIMARY KEY,
    image            TEXT NOT NULL,
    goal             TEXT NOT NULL DEFAULT '',
    model            TEXT NOT NULL DEFAULT '',
    grants           TEXT NOT NULL DEFAULT '{}',
    isolation        TEXT NOT NULL,
    checkpoint       TEXT NOT NULL,
    parent_id        TEXT NOT NULL DEFAULT '',
    parent_turn      INTEGER NOT NULL DEFAULT 0,
    state            TEXT NOT NULL,
    activation_count INTEGER NOT NULL DEFAULT 0,
    last_turn        INTEGER NOT NULL DEFAULT 0,
    last_seq         INTEGER NOT NULL DEFAULT 0,
    last_checkpoint  TEXT NOT NULL DEFAULT '',
    metadata         TEXT NOT NULL DEFAULT '{}',
    crash_count      INTEGER NOT NULL DEFAULT 0,
    quarantined      INTEGER NOT NULL DEFAULT 0,
    quarantined_until INTEGER NOT NULL DEFAULT 0,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS activations (
    id           TEXT PRIMARY KEY,
    agent_id     TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    number       INTEGER NOT NULL,
    image_digest TEXT NOT NULL,
    grants       TEXT NOT NULL,
    isolation    TEXT NOT NULL,
    token        TEXT NOT NULL,
    started_at   INTEGER NOT NULL,
    ended_at     INTEGER,
    end_reason   TEXT NOT NULL DEFAULT '',
    UNIQUE(agent_id, number)
);

CREATE TABLE IF NOT EXISTS checkpoints (
    id           TEXT PRIMARY KEY,
    agent_id     TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    turn         INTEGER NOT NULL,
    -- boundary_seq is the sequence of the turn boundary this checkpoint was
    -- produced at: the point in the log the workspace it holds stood at. It is
    -- what makes "the workspace as of turn N" a range query with one answer.
    -- 0 means "not known", for rows written before the column existed and not
    -- yet backfilled from the log.
    boundary_seq INTEGER NOT NULL DEFAULT 0,
    parent_id    TEXT NOT NULL DEFAULT '',
    manifest     TEXT NOT NULL,
    created_at   INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_activations_agent ON activations(agent_id, number);
CREATE INDEX IF NOT EXISTS idx_checkpoints_agent ON checkpoints(agent_id, turn);
CREATE INDEX IF NOT EXISTS idx_checkpoints_boundary ON checkpoints(agent_id, boundary_seq);
CREATE INDEX IF NOT EXISTS idx_checkpoints_created ON checkpoints(agent_id, created_at);
CREATE INDEX IF NOT EXISTS idx_agents_state ON agents(state);

CREATE TABLE IF NOT EXISTS agent_inputs (
    id         TEXT PRIMARY KEY,
    agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    body       TEXT NOT NULL,
    kind       TEXT NOT NULL DEFAULT 'user',
    created_at INTEGER NOT NULL,
    consumed    INTEGER NOT NULL DEFAULT 0,
    consumed_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_inputs_agent ON agent_inputs(agent_id, created_at);
`

// now returns the current time; factored for tests that may inject a clock.
func now() time.Time { return time.Now().UTC() }

// newID returns a fresh random id (UUID v4, no dashes for compactness).
func newID() string {
	id := uuid.NewString()
	return id
}

// timeUnix converts a UnixNano timestamp to a UTC time, handling zero.
func timeUnix(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}
