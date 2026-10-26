// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Xustalis/OpenPanda/internal/projects"
)

// volumeRoot is the filesystem root of whatever drive the test runs on: "/" on
// Unix, "C:\" on Windows, where a bare "/" is not even absolute.
func volumeRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.VolumeName(wd) + string(filepath.Separator)
}

// TestWorkDirPinBreadth locks the breadth rule at both places a pin is
// persisted. A task's work dir is the writable root of its sandbox
// (security.Policy.WorkDir), so pinning a volume root or the home directory
// hands the run the machine — and the delegate marker's workdir is generated
// by a model rather than typed, which is exactly why the check cannot live
// only in the CLI and the panel.
func TestWorkDirPinBreadth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("no home directory to test with: %v", err)
	}
	tooBroad := []struct{ name, dir string }{
		{"volume root", volumeRoot(t)},
		{"home directory", home},
	}

	for _, tc := range tooBroad {
		t.Run(tc.name+"/enqueue", func(t *testing.T) {
			c := queueTestCore(t)
			_, err := c.Enqueue(ctx, TaskInput{Title: "wide", Intent: "x", WorkDir: tc.dir}, DefaultQueueSpec())
			if !errors.Is(err, projects.ErrWorkDirTooBroad) {
				t.Fatalf("enqueue pinning %q = %v, want ErrWorkDirTooBroad", tc.dir, err)
			}
		})
		t.Run(tc.name+"/child", func(t *testing.T) {
			c := newCore(t, "workdir-node")
			if err := c.Register(ctx); err != nil {
				t.Fatalf("register: %v", err)
			}
			parent, _, _, err := c.createTask(ctx, TaskInput{Title: "parent", Intent: "orchestrate"})
			if err != nil {
				t.Fatalf("create parent: %v", err)
			}
			if _, err := c.SpawnChildTask(ctx, parent.TaskID, TaskInput{
				Title: "wide child", Intent: "sub work", WorkDir: tc.dir,
			}); !errors.Is(err, projects.ErrWorkDirTooBroad) {
				t.Fatalf("spawn pinning %q = %v, want ErrWorkDirTooBroad", tc.dir, err)
			}
		})
	}

	// An ordinary enough directory still passes: the rule is about breadth,
	// not ownership, so a task that names its own business-domain tree runs.
	c := queueTestCore(t)
	pin := filepath.Join(t.TempDir(), "feat-auth")
	if _, err := c.Enqueue(ctx, TaskInput{Title: "narrow", Intent: "x", WorkDir: pin}, DefaultQueueSpec()); err != nil {
		t.Fatalf("enqueue pinning %q: %v", pin, err)
	}
}
