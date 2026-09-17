// Package fsx holds the filesystem operations that have to hold up when a run
// is interrupted or when a path came from a file rather than from the walk that
// produced it. It is to the filesystem what gitx is to git.
package fsx

import (
	"os"
	"path/filepath"
	"strings"
)

// WriteFile writes data to path through a sibling temporary file, so no reader
// and no interrupted run can observe the destination half-written.
//
// os.WriteFile opens with O_TRUNC: between the truncation and the last byte the
// file on disk is empty or partial. That window is not theoretical here — the
// SessionEnd hook is cancelled as a matter of routine — and a memory truncated
// in it is pushed to the remote like any other, where an append-only history
// keeps the mutilated version as the current one forever.
//
// The temporary file is a sibling of the destination so that the rename stays
// inside one filesystem, which is where it is atomic. It is removed when the
// write fails, because the memory directories are listed by extension and a
// stray file should not pile up there.
//
// This does not survive a power cut: nothing here calls fsync, so the rename can
// reach the directory before the data reaches the disk.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Within reports whether path lies inside root.
//
// The comparison is on path boundaries, not on strings: ".../personal-other"
// has ".../personal" as a string prefix while being nowhere inside it. An empty
// root contains nothing, so a layer that is not available cannot be the reason a
// path is accepted.
func Within(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	// Only a ".." that is a whole path element means "outside": a sibling named
	// "..keep" is relative to root as "..keep", and is inside it.
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
