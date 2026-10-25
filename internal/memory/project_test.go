// SPDX-License-Identifier: AGPL-3.0-or-later

package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{"panda", "demo-project", "my-project", "a.b"}
	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", n, err)
		}
	}
	invalid := []string{"", ".", "..", "../etc", "a/b", `a\b`, "/etc"}
	for _, n := range invalid {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", n)
		}
	}
}

func TestProjectsSaveLoad(t *testing.T) {
	p := NewProjects(t.TempDir())
	m := MemFile{Entries: []string{"Go core + Python glue", "deploy to Orange Pi"}}
	if err := p.Save("panda", m); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := p.Load("panda")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Entries) != 2 || got.Entries[0] != "Go core + Python glue" {
		t.Errorf("round-trip mismatch: %v", got.Entries)
	}
	if got.Limit != ProjectCharLimit {
		t.Errorf("load should apply the project limit, got %d", got.Limit)
	}
}

func TestProjectsLoadMissing(t *testing.T) {
	p := NewProjects(t.TempDir())
	got, err := p.Load("nope")
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if len(got.Entries) != 0 {
		t.Errorf("missing project should yield empty MemFile, got %v", got.Entries)
	}
}

func TestProjectsPathRejectsTraversal(t *testing.T) {
	p := NewProjects(t.TempDir())
	if _, err := p.Path("../escape"); err == nil {
		t.Errorf("Path with traversal should error")
	}
}

// TestProjectsSaveEnforcesCap verifies Save rejects an over-limit project memory
// rather than silently truncating it (P2-11), mirroring Hermes.save.
func TestProjectsSaveEnforcesCap(t *testing.T) {
	p := NewProjects(t.TempDir())
	m := MemFile{Entries: []string{strings.Repeat("x", ProjectCharLimit+100)}}
	if err := p.Save("panda", m); !errors.Is(err, ErrOverLimit) {
		t.Fatalf("save over cap: got %v, want ErrOverLimit", err)
	}
	got, err := p.Load("panda")
	if err != nil {
		t.Fatalf("load after failed save: %v", err)
	}
	if len(got.Entries) != 0 {
		t.Errorf("failed save should write nothing, got %v", got.Entries)
	}
}

// TestProjectsRenameCarriesMemory pins the copy-then-drop contract: the new
// name has the entries, the old directory is gone, and a failed copy leaves the
// old one in place (the failure case is covered by the containment check
// below — an invalid new name must not touch the source).
func TestProjectsRenameCarriesMemory(t *testing.T) {
	p := NewProjects(t.TempDir())
	if err := p.Save("old", MemFile{Entries: []string{"one", "two"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := p.Rename("old", "new"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, err := p.Load("new")
	if err != nil {
		t.Fatalf("load new: %v", err)
	}
	if len(got.Entries) != 2 || got.Entries[0] != "one" {
		t.Fatalf("new entries = %v", got.Entries)
	}
	if _, err := os.Stat(filepath.Join(p.root, "old")); !os.IsNotExist(err) {
		t.Fatalf("old directory survived the rename: %v", err)
	}
}

func TestProjectsRenameRejectsBadNewName(t *testing.T) {
	p := NewProjects(t.TempDir())
	if err := p.Save("keep", MemFile{Entries: []string{"payload"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := p.Rename("keep", "../escape"); err == nil {
		t.Fatal("expected a traversal-shaped new name to be rejected")
	}
	got, err := p.Load("keep")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("source lost by a rejected rename: %v", got.Entries)
	}
}

func TestProjectsRenameSameNameIsNoop(t *testing.T) {
	p := NewProjects(t.TempDir())
	if err := p.Save("same", MemFile{Entries: []string{"entry-1"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := p.Rename("same", "same"); err != nil {
		t.Fatalf("rename same name: %v", err)
	}
	got, err := p.Load("same")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Entries) != 1 || got.Entries[0] != "entry-1" {
		t.Fatalf("entries lost by same-name rename: %v", got.Entries)
	}
}
