package store

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/mrn-dk/stonewall/internal/model"
)

// FileNode is one entry in a read-only checkpoint workspace listing. It is the
// view the dashboard's workspace column renders: a path, whether it is a
// directory, its size, and (for files) the chunk digests it reassembles from.
type FileNode struct {
	Path   string   `json:"path"`
	IsDir  bool     `json:"is_dir"`
	Size   int64    `json:"size"`
	Mode   uint32   `json:"mode"`
	Chunks []string `json:"chunks,omitempty"`
}

// BrowseCheckpoint returns a read-only file tree of a checkpoint's workspace,
// reconstructed from the content-addressed manifest. It does NOT touch the
// agent's live workspace on disk — distinct from MaterializeWorkspace/restore,
// which rewrite the live volume. Used by the dashboard's workspace column.
func (s *Store) BrowseCheckpoint(cp *model.Checkpoint) []FileNode {
	nodes := make([]FileNode, 0, len(cp.Manifest))
	keys := make([]string, 0, len(cp.Manifest))
	for k := range cp.Manifest {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, p := range keys {
		e := cp.Manifest[p]
		isDir := e.Mode&os.ModeDir != 0
		n := FileNode{Path: p, IsDir: isDir, Size: e.Size, Mode: uint32(e.Mode.Perm()), Chunks: e.Chunks}
		nodes = append(nodes, n)
	}
	return nodes
}

// ReadCheckpointFile reconstructs one file's contents from a checkpoint by
// concatenating its content-addressed chunks. It streams to w without
// materializing the whole file in memory when possible. It does NOT touch the
// live workspace. pathKey is the manifest key (relative path). Returns
// ErrNotFound if the path is not in the manifest or is a directory.
func (s *Store) ReadCheckpointFile(cp *model.Checkpoint, pathKey string, w io.Writer) error {
	pathKey = cleanPathKey(pathKey)
	entry, ok := cp.Manifest[pathKey]
	if !ok {
		return ErrNotFound
	}
	if entry.Mode&os.ModeDir != 0 {
		return fmt.Errorf("path %q is a directory", pathKey)
	}
	for _, digest := range entry.Chunks {
		data, err := s.getChunk(digest)
		if err != nil {
			return fmt.Errorf("read chunk %s: %w", digest, err)
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// CheckpointFileEntry returns the manifest entry for one path (for the
// dashboard to get size/mode without reading contents).
func (s *Store) CheckpointFileEntry(cp *model.Checkpoint, pathKey string) (model.FileEntry, error) {
	pathKey = cleanPathKey(pathKey)
	e, ok := cp.Manifest[pathKey]
	if !ok {
		return model.FileEntry{}, ErrNotFound
	}
	return e, nil
}

// cleanPathKey normalises a path key to the manifest's forward-slash form and
// rejects absolute or escaping paths.
func cleanPathKey(p string) string {
	if p == "" {
		return p
	}
	// reject absolute and parent escapes — manifest keys are relative.
	clean := path.Clean(p)
	if strings.HasPrefix(clean, "/") {
		clean = strings.TrimPrefix(clean, "/")
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." {
			return p // leave as-is; manifest lookup will simply miss
		}
	}
	return clean
}

// errIs reports whether err is the store's not-found sentinel.
func errIsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
