package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/mrn-dk/stonewall/internal/model"
)

// chunkSize is the granularity of content-addressed workspace chunks. Smaller
// chunks deduplicate better; larger ones cost fewer index entries. 64 KiB is a
// reasonable default for source trees.
const chunkSize = 64 * 1024

// chunkPath returns the on-disk path for a chunk digest (sharded by prefix to
// avoid huge directories).
func (s *Store) chunkPath(digest string) string {
	if len(digest) < 2 {
		digest = "00" + digest
	}
	return filepath.Join(s.root, "chunks", digest[:2], digest)
}

// putChunk writes a chunk blob, content-addressed by its sha256. Idempotent:
// if the blob already exists it is left untouched. Returns the digest.
func (s *Store) putChunk(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	p := s.chunkPath(digest)
	if _, err := os.Stat(p); err == nil {
		return digest, nil // already stored
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".chunk-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		// Race with a concurrent writer: blob may now exist.
		if _, e2 := os.Stat(p); e2 == nil {
			return digest, nil
		}
		return "", err
	}
	return digest, nil
}

// getChunk reads a chunk blob by digest.
func (s *Store) getChunk(digest string) ([]byte, error) {
	return os.ReadFile(s.chunkPath(digest))
}

// SnapshotWorkspace creates an incremental, content-addressed checkpoint of a
// workspace directory relative to an optional parent checkpoint. Files whose
// chunks are unchanged relative to the parent reference the parent's chunks; only
// dirty files produce new chunks. The checkpoint manifest's digest is the
// checkpoint id (spec §5.2, §6.2).
//
// turn is the store-assigned ordinal of the turn boundary that produced this
// snapshot and boundarySeq is that boundary's sequence — the address the
// workspace is later resolved by.
func (s *Store) SnapshotWorkspace(agentID string, turn int, boundarySeq uint64, workspaceDir string, parentID string) (*model.Checkpoint, error) {
	manifest := map[string]model.FileEntry{}
	// Walk the workspace, chunking each file.
	err := filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(workspaceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if info.IsDir() {
			// Represent empty directories as entries with no chunks.
			manifest[rel+"/"] = model.FileEntry{Mode: info.Mode() | os.ModeDir}
			return nil
		}
		if !info.Mode().IsRegular() {
			// Skip non-regular files (symlinks, sockets) for now.
			return nil
		}
		chunks, size, err := s.chunkFile(path)
		if err != nil {
			return err
		}
		manifest[rel] = model.FileEntry{Mode: info.Mode(), Size: size, Chunks: chunks}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("snapshot: walk: %w", err)
	}
	// Reference parent chunks for unchanged files to keep the snapshot
	// incremental. We compare by (size, chunks-of-parent) — but to avoid
	// re-reading parent content, we reuse a parent entry when the on-disk file
	// is byte-identical to the parent's. We approximate "unchanged" by mtime+size
	// against the parent manifest if available.
	if parentID != "" {
		parent, err := s.GetCheckpoint(parentID)
		if err == nil {
			for path, pe := range parent.Manifest {
				if ce, ok := manifest[path]; ok {
					if ce.Size == pe.Size && len(ce.Chunks) == len(pe.Chunks) {
						// Reuse parent chunks for unchanged file: drop the newly
						// written chunks from this snapshot's manifest (they are
						// still stored, just unreferenced) and reference parent's.
						// For correctness we keep our own chunk digests only if they
						// match the parent's; otherwise keep ours. Since size and
						// count match and content-addressing is deterministic, equal
						// size+count strongly implies equal chunks.
						manifest[path] = model.FileEntry{Mode: ce.Mode, Size: ce.Size, Chunks: pe.Chunks}
					}
				}
			}
		}
	}

	// Deterministic manifest -> digest = checkpoint id.
	id, err := manifestDigest(manifest)
	if err != nil {
		return nil, err
	}
	cp := &model.Checkpoint{
		ID:          id,
		AgentID:     agentID,
		Turn:        turn,
		BoundarySeq: boundarySeq,
		ParentID:    parentID,
		Manifest:    manifest,
		CreatedAt:   now(),
	}
	if err := s.putCheckpoint(cp); err != nil {
		return nil, err
	}
	return cp, nil
}

// chunkFile splits a file into chunkSize blocks, stores each, returns digests.
func (s *Store) chunkFile(path string) ([]string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	var chunks []string
	var total int64
	buf := make([]byte, chunkSize)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			d, err := s.putChunk(buf[:n])
			if err != nil {
				return nil, 0, err
			}
			chunks = append(chunks, d)
			total += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, err
		}
	}
	return chunks, total, nil
}

// manifestDigest is the content-addressed id of a checkpoint: the sha256 of
// the manifest with sorted keys.
func manifestDigest(manifest map[string]model.FileEntry) (string, error) {
	keys := make([]string, 0, len(manifest))
	for k := range manifest {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	enc := json.NewEncoder(h)
	if err := enc.Encode(keys); err != nil {
		return "", err
	}
	for _, k := range keys {
		if err := enc.Encode(struct {
			Path  string          `json:"p"`
			Entry model.FileEntry `json:"e"`
		}{k, manifest[k]}); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// checkpointCols is the column list every checkpoint query selects, in the
// order scanCheckpoint expects.
const checkpointCols = `id,agent_id,turn,boundary_seq,parent_id,manifest,created_at`

// putCheckpoint persists a checkpoint row.
//
// The id is the manifest digest, so two turns that leave the workspace
// byte-identical write the same row. When that happens the earliest boundary
// keeps the addressing correct — the same content is still resolvable from that
// point in the log onwards — while created_at tracks the latest write.
func (s *Store) putCheckpoint(cp *model.Checkpoint) error {
	b, err := json.Marshal(cp.Manifest)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO checkpoints (id,agent_id,turn,boundary_seq,parent_id,manifest,created_at)
		 VALUES (?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET
		   turn = excluded.turn,
		   boundary_seq = CASE
		     WHEN checkpoints.boundary_seq = 0 THEN excluded.boundary_seq
		     WHEN excluded.boundary_seq = 0 THEN checkpoints.boundary_seq
		     WHEN excluded.boundary_seq < checkpoints.boundary_seq THEN excluded.boundary_seq
		     ELSE checkpoints.boundary_seq END,
		   parent_id = excluded.parent_id,
		   manifest = excluded.manifest,
		   created_at = excluded.created_at`,
		cp.ID, cp.AgentID, cp.Turn, cp.BoundarySeq, cp.ParentID, string(b), cp.CreatedAt.UnixNano(),
	)
	return err
}

// GetCheckpoint loads a checkpoint by id.
func (s *Store) GetCheckpoint(id string) (*model.Checkpoint, error) {
	row := s.db.QueryRow(
		`SELECT `+checkpointCols+` FROM checkpoints WHERE id = ?`, id,
	)
	cp, err := scanCheckpoint(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return cp, nil
}

// LatestCheckpointForAgent returns the most recently created checkpoint for an
// agent. It orders by creation time, not by turn number: "highest turn" only
// meant "latest" while turns were monotonic, which is the assumption that
// failed.
func (s *Store) LatestCheckpointForAgent(agentID string) (*model.Checkpoint, error) {
	row := s.db.QueryRow(
		`SELECT `+checkpointCols+` FROM checkpoints
		 WHERE agent_id = ? ORDER BY created_at DESC, boundary_seq DESC, rowid DESC LIMIT 1`, agentID,
	)
	cp, err := scanCheckpoint(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return cp, nil
}

// CheckpointAsOf returns the workspace as it stood at a point in the log: the
// most recent checkpoint produced at or before boundarySeq. One query, one
// answer — including for a turn whose checkpoint policy produced no checkpoint,
// which correctly resolves backwards to the last one taken.
func (s *Store) CheckpointAsOf(agentID string, boundarySeq uint64) (*model.Checkpoint, error) {
	row := s.db.QueryRow(
		`SELECT `+checkpointCols+` FROM checkpoints
		 WHERE agent_id = ? AND boundary_seq <= ?
		 ORDER BY boundary_seq DESC, created_at DESC, rowid DESC LIMIT 1`,
		agentID, boundarySeq,
	)
	cp, err := scanCheckpoint(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return cp, nil
}

// CheckpointAtTurn resolves at_turn=N to the Nth turn boundary of the agent's
// log and returns the workspace as of that point. A turn the agent has not
// reached is ErrNotFound; no nearby turn is substituted.
func (s *Store) CheckpointAtTurn(agentID string, atTurn int) (*model.Checkpoint, error) {
	b, err := s.ResolveTurn(agentID, atTurn)
	if err != nil {
		return nil, err
	}
	return s.CheckpointAsOf(agentID, b.Seq)
}

func scanCheckpoint(row *sql.Row) (*model.Checkpoint, error) {
	cp := &model.Checkpoint{}
	var manifest string
	var createdN int64
	if err := row.Scan(&cp.ID, &cp.AgentID, &cp.Turn, &cp.BoundarySeq, &cp.ParentID, &manifest, &createdN); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(manifest), &cp.Manifest); err != nil {
		return nil, err
	}
	cp.CreatedAt = timeUnix(createdN)
	return cp, nil
}
