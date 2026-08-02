package store

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mrn-dk/stonewall/internal/model"
)

// workspaceDir returns the per-agent workspace volume path.
func (s *Store) workspaceDir(agentID string) string {
	return filepath.Join(s.root, "workspaces", agentID)
}

// WorkspacePath returns the absolute workspace directory for an agent.
func (s *Store) WorkspacePath(agentID string) string {
	return s.workspaceDir(agentID)
}

// MaterializeWorkspace restores a checkpoint into an agent's workspace
// directory, reassembling files from content-addressed chunks. Used for resume
// ("restore the workspace to the checkpoint referenced by the last turn") and
// for forking (a fork's volume starts as a CoW view of the parent's
// checkpoint). Existing workspace contents are removed first (spec §5.3, §6.1).
func (s *Store) MaterializeWorkspace(agentID string, cp *model.Checkpoint) error {
	dir := s.workspaceDir(agentID)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("materialize: clear workspace: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if cp == nil {
		return nil // empty workspace
	}
	// Sort manifest entries so directories are created before their files.
	paths := sortedKeys(cp.Manifest)
	for _, p := range paths {
		entry := cp.Manifest[p]
		isDir := entry.Mode&os.ModeDir != 0
		full := filepath.Join(dir, p)
		if isDir {
			if err := os.MkdirAll(full, entry.Mode.Perm()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, entry.Mode.Perm())
		if err != nil {
			return err
		}
		for _, digest := range entry.Chunks {
			data, err := s.getChunk(digest)
			if err != nil {
				f.Close()
				return fmt.Errorf("materialize: chunk %s: %w", digest, err)
			}
			if _, err := f.Write(data); err != nil {
				f.Close()
				return err
			}
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

// InitEmptyWorkspace creates an empty workspace directory for an agent.
func (s *Store) InitEmptyWorkspace(agentID string) error {
	return os.MkdirAll(s.workspaceDir(agentID), 0o755)
}

// ForkWorkspace creates a fork's workspace as a copy-on-write view of a parent
// checkpoint. In this single-node content-addressed realization, CoW is
// structural: the fork materializes the parent checkpoint (chunks are shared
// on disk by digest), and only dirty files written during the fork's run
// produce new chunks at checkpoint time. This is the "content-addressed
// manifest referencing shared blobs" mechanism (spec §6.2), with sub-100ms
// target feasible because materialization reads from already-present chunks.
func (s *Store) ForkWorkspace(childID string, parentCP *model.Checkpoint) error {
	return s.MaterializeWorkspace(childID, parentCP)
}

// EnsureWorkspace returns the workspace directory, creating it empty if absent.
func (s *Store) EnsureWorkspace(agentID string) (string, error) {
	dir := s.workspaceDir(agentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func sortedKeys(m map[string]model.FileEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Directories (paths ending in "/") sort before their contents because "/"
	// sorts before most filename characters, but be explicit.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && lessPath(keys[j], keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// lessPath orders directory entries before their children.
func lessPath(a, b string) bool {
	return a < b
}
