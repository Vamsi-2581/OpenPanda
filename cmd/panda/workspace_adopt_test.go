// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Xustalis/OpenPanda/internal/projects"
	"github.com/Xustalis/OpenPanda/internal/storage"
)

// TestAdoptRefusesHome pins the root cause of the "every task fails with work
// dir is too broad" incident. Adoption used to mint a project row for any
// marker-bearing directory, and $HOME carries markers on most developer
// machines (~/.claude alone is enough). The row was legal when written; the
// work-dir breadth rule landed later and made every task under it fail at
// creation, with nothing in the error naming the project that caused it.
func TestAdoptRefusesHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("no home directory to test with: %v", err)
	}
	s := newProjectStore(t)
	if name := adoptWorkspaceProject(s, home); name != "" {
		t.Fatalf("adopting %q = %q, want refusal", home, name)
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Fatalf("refused adoption still wrote %d row(s)", len(list))
	}
	if active, _ := s.Active(); active != "" {
		t.Fatalf("refused adoption set active = %q", active)
	}
}

// TestAdoptRefusesAncestorOfHome: the breadth rule rejects every ancestor of
// $HOME too, so adoption must stop at the same boundary rather than mint a row
// the executor will refuse.
func TestAdoptRefusesAncestorOfHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("no home directory to test with: %v", err)
	}
	s := newProjectStore(t)
	if name := adoptWorkspaceProject(s, filepath.Dir(home)); name != "" {
		t.Fatalf("adopting the parent of home = %q, want refusal", name)
	}
}

// TestProjectForDirSkipsTooBroadRow: a legacy row pointing at $HOME must not
// claim every directory beneath it. Before this, launching in any ordinary
// workspace under the home tree resolved to the home project, so the task ran
// with a work dir the executor refuses.
func TestProjectForDirSkipsTooBroadRow(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("no home directory to test with: %v", err)
	}
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := projects.NewStore(db)
	// The row is written straight into the table the way a pre-rule build left
	// it; Store.Create refuses this path today.
	if _, err := db.Exec(
		`INSERT INTO projects (name, work_dir, description, created_at, updated_at)
		 VALUES ('home', ?, '', 0, 0)`, home); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	deep := filepath.Join(home, "some-workspace")
	if name, _ := projectForDir(s, deep); name != "" {
		t.Fatalf("legacy home row claimed %q as %q", deep, name)
	}
}
