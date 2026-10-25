// SPDX-License-Identifier: AGPL-3.0-or-later

package sessions

import (
	"path/filepath"

	"github.com/Xustalis/OpenPanda/internal/fslock"
)

// lockStore takes an advisory exclusive lock on the sessions root so a
// read-modify-write cycle is atomic ACROSS PROCESSES, not just across
// goroutines. The CLI and the web panel (and the standalone sidecar) share one
// sessions directory: without this, two processes could each load the old JSON
// and the last writer would silently drop the other's turn.
//
// The lock is advisory — every writer in this package takes it, and a foreign
// process that ignores it is outside the contract. Blocking mode, so a writer
// queues for its turn instead of dropping its change.
func lockStore(root string) (func(), error) {
	if root == "" {
		return func() {}, nil
	}
	return fslock.Acquire(filepath.Join(root, ".lock"), true)
}
