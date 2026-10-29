// SPDX-License-Identifier: AGPL-3.0-or-later

package projects

import (
	"os"
	"testing"
)

// TestUsableWorkDirDropsTooBroad covers the migration hazard: rows written
// before ValidateWorkDir existed can name the home directory (home-directory
// adoption created them). Handing that path to the executor fails every
// dependent task at creation with "work dir is too broad", and nothing in that
// message points at the project row — so readers must not propagate it.
func TestUsableWorkDirDropsTooBroad(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("no home directory to test with: %v", err)
	}
	// The row is written directly: Create and Update both refuse this path now,
	// which is exactly why the legacy shape has to be simulated.
	if p := (Project{WorkDir: home}); p.UsableWorkDir() != "" {
		t.Fatalf("UsableWorkDir(%q) = %q, want empty", home, p.UsableWorkDir())
	}
	// An ordinary tree still answers with itself.
	dir := t.TempDir()
	if got := (Project{WorkDir: dir}).UsableWorkDir(); got != dir {
		t.Fatalf("UsableWorkDir(%q) = %q, want itself", dir, got)
	}
	// No work dir at all stays empty, not an error.
	if got := (Project{}).UsableWorkDir(); got != "" {
		t.Fatalf("UsableWorkDir(\"\") = %q, want empty", got)
	}
}

// TestUsableWorkDirSurvivesRead pins the same rule on a persisted row, so a
// future reader that reaches for WorkDir directly has a test naming the hazard.
func TestUsableWorkDirSurvivesRead(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	if _, err := s.Create("real", dir, ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	p, err := s.Get("real")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := p.UsableWorkDir(); got != dir {
		t.Fatalf("UsableWorkDir = %q, want %q", got, dir)
	}
}
