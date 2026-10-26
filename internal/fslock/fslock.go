// SPDX-License-Identifier: AGPL-3.0-or-later

// Package fslock takes an exclusive advisory lock on a file, so a
// read-modify-write cycle is atomic ACROSS PROCESSES and not only across
// goroutines.
//
// The pattern it exists for is the whole-file store: something loads a file
// into memory, changes it, and writes the whole thing back. Two writers that
// overlap lose one of the two changes, and the loss is silent — the file still
// parses, it is just missing an entry nobody can point at. The daemon, the CLI
// and the web console all open the same stores, and inside one daemon the
// subagents of a single round run in parallel, so "two writers" is the normal
// case rather than an edge one.
//
// The lock is advisory: it coordinates the callers that take it. A foreign
// program that writes the same file without looking is outside the contract.
package fslock

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// ErrHeld reports that another holder has the lock and the caller asked not to
// wait. It is contention, never a failure: a caller that probes ("is this
// identity taken?") maps it to its own sentinel, while one that needs the lock
// uses blocking mode instead.
var ErrHeld = errors.New("fslock: lock held by another process")

// Acquire takes an exclusive advisory lock on path, creating the file and its
// directory as needed. blocking=false returns ErrHeld instead of waiting.
//
// The returned release unlocks and closes the file; it is safe to call exactly
// once. A platform without a lock primitive, and a filesystem that reports it
// has none, both degrade to a no-op release rather than failing the caller —
// the caller's own mutex still serializes goroutines there, and refusing to
// write a user's memory because their data directory sits on an NFS mount
// would trade a rare silent loss for a guaranteed loud breakage. Any other
// error (a permission, a bad handle) is returned: those are the caller's
// problem to surface.
func Acquire(path string, blocking bool) (release func(), err error) {
	if path == "" {
		return func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	held, unsupported, err := tryLock(f, blocking)
	switch {
	case unsupported:
		// No lock primitive here: the store still works, one writer at a time
		// per process. Reported by the platform shim, not an error.
		_ = f.Close()
		return func() {}, nil
	case err != nil:
		_ = f.Close()
		return nil, err
	case held:
		_ = f.Close()
		return nil, ErrHeld
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = unlock(f)
			_ = f.Close()
		})
	}, nil
}
