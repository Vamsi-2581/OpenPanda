// SPDX-License-Identifier: AGPL-3.0-or-later

package memory

import (
	"path/filepath"

	"github.com/Xustalis/OpenPanda/internal/fslock"
)

// lockRoot serializes a whole-file memory store across processes, one coarse
// lock per store root rather than per file: every write here rewrites exactly
// one small file, so a per-file lock would cost the same and buy nothing.
//
// The scope of what it protects is the read-modify-write, not the write. The
// write was already atomic (util.WriteFileAtomic), which stops a torn file but
// not a lost update: two writers that each load, change and save leave the
// second one's view of the file intact and erase the first one's entry, with
// nothing on disk to show it happened. Two subagents of one round adding a
// memory entry at the same moment is the ordinary case that loses data here,
// and the CLI editing MEMORY.md while the daemon writes it is the other.
//
// It is taken BEFORE the in-process mutex, the order internal/sessions already
// uses. One order everywhere is what makes the pair deadlock-free: a goroutine
// waits on the file lock while holding nothing, so it can never be the one
// blocking the holder of the mutex it needs.
func lockRoot(root string) (func(), error) {
	if root == "" {
		return func() {}, nil
	}
	return fslock.Acquire(filepath.Join(root, ".lock"), true)
}
