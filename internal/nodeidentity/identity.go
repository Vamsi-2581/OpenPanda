// SPDX-License-Identifier: AGPL-3.0-or-later

// Package nodeidentity owns the process-wide identity lock for a running node.
// It deliberately keys locks by node kind and stable identity: one physical
// node per host, while a VM on that host may hold a separate VM lock.
package nodeidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Xustalis/OpenPanda/internal/fslock"
)

const (
	KindPhysical = "physical"
	KindVM       = "vm"
)

// ErrAlreadyRunning identifies a live process holding the same node identity.
// Callers should use errors.Is instead of parsing platform-specific errors.
var ErrAlreadyRunning = errors.New("node identity already running")

// Lock is held for the lifetime of a daemon process.
type Lock struct {
	release func()
	path    string
}

// Acquire obtains the exclusive local lock for kind/identity. The OS-level
// advisory lock is released automatically if the process exits unexpectedly.
//
// Only contention is reported as ErrAlreadyRunning. A lock that cannot be taken
// for any other reason — an unreadable config directory, a volume that refuses
// locks — comes back as itself, because callers draw opposite conclusions from
// the two: a daemon treats ErrAlreadyRunning as "another node is up" and stands
// down, and Held reports it as "the row has an owner". Reporting an I/O error
// as contention is how a broken environment turned into a false demotion.
func Acquire(kind, identity string) (*Lock, error) {
	kind = strings.TrimSpace(kind)
	identity = strings.TrimSpace(identity)
	if kind == "" {
		kind = KindPhysical
	}
	if kind != KindPhysical && kind != KindVM {
		return nil, fmt.Errorf("unsupported node kind %q", kind)
	}
	if identity == "" {
		return nil, fmt.Errorf("node identity is empty")
	}
	path, err := lockPath(kind, identity)
	if err != nil {
		return nil, err
	}
	release, err := fslock.Acquire(path, false)
	if err != nil {
		if errors.Is(err, fslock.ErrHeld) {
			return nil, fmt.Errorf("node %s identity %q: %w", kind, identity, ErrAlreadyRunning)
		}
		return nil, fmt.Errorf("node %s identity %q: lock %s: %w", kind, identity, path, err)
	}
	return &Lock{release: release, path: path}, nil
}

// Held reports whether another process currently owns the identity lock.
func Held(kind, identity string) (bool, error) {
	lock, err := Acquire(kind, identity)
	if err == nil {
		_ = lock.Release()
		return false, nil
	}
	if errors.Is(err, ErrAlreadyRunning) {
		return true, nil
	}
	return false, err
}

// Release releases the OS lock. The lock file itself is intentionally kept:
// that makes startup races deterministic and avoids stale PID-file recovery.
func (l *Lock) Release() error {
	if l == nil || l.release == nil {
		return nil
	}
	l.release()
	l.release = nil
	return nil
}

// lockRootOverride redirects the lock directory for tests: os.UserConfigDir()
// ignores XDG_CONFIG_HOME on darwin, so the env-var approach alone leaks test
// locks into the real user directory on macOS.
var lockRootOverride string

func lockPath(kind, identity string) (string, error) {
	root := lockRootOverride
	if root == "" {
		var err error
		root, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve node lock directory: %w", err)
		}
	}
	sum := sha256.Sum256([]byte(identity))
	return filepath.Join(root, "openpanda", "locks", kind+"-"+hex.EncodeToString(sum[:12])+".lock"), nil
}
