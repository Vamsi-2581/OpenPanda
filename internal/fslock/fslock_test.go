// SPDX-License-Identifier: AGPL-3.0-or-later

package fslock

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// TestExcludesSecondHolder pins the contract the whole package exists for: a
// second open of the same lock file must not get the lock while the first
// holds it. flock and LockFileEx both key on the open file description, so this
// also holds inside one process — which is exactly the case that used to be
// covered only by an in-memory mutex, per store instance.
func TestExcludesSecondHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".lock")
	release, err := Acquire(path, true)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	if _, err := Acquire(path, false); !errors.Is(err, ErrHeld) {
		t.Fatalf("second non-blocking acquire = %v, want ErrHeld", err)
	}
}

// TestBlockingWaitsForRelease pins that blocking mode waits instead of failing:
// the point of the default is that a writer queues rather than dropping its
// change, so returning ErrHeld there would be a silent data loss.
func TestBlockingWaitsForRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".lock")
	first, err := Acquire(path, true)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		release, err := Acquire(path, true)
		if err != nil {
			return // reported by the timeout assertion below
		}
		close(acquired)
		release()
	}()

	select {
	case <-acquired:
		t.Fatal("blocking acquire returned while the lock was held")
	case <-time.After(100 * time.Millisecond):
	}

	first()
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("blocking acquire never returned after release")
	}
}

// TestReleaseAllowsReacquire covers the obvious leak: a store that saves twice
// in a row must not deadlock itself on the second save.
func TestReleaseAllowsReacquire(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".lock")
	release, err := Acquire(path, true)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	release()

	again, err := Acquire(path, true)
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	again()
}

// TestReleaseIdempotent pins that calling release multiple times is safe and
// does not panic, error, or unlock recycled descriptors.
func TestReleaseIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".lock")
	release, err := Acquire(path, true)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	release()
	release() // second call must be a safe no-op
	release() // third call likewise
}

// TestAcquireCreatesParent keeps the caller free of MkdirAll: a lock inside a
// store root that does not exist yet is the first-save case.
func TestAcquireCreatesParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory", "nested", ".lock")
	release, err := Acquire(path, true)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	release()
}

// TestEmptyPathIsNoop pins the escape hatch: a store with no root configured
// (tests, or a caller that deliberately disabled the store) must not fail.
func TestEmptyPathIsNoop(t *testing.T) {
	release, err := Acquire("", true)
	if err != nil {
		t.Fatalf("acquire empty path: %v", err)
	}
	release()
}
