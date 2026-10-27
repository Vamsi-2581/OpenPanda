// SPDX-License-Identifier: AGPL-3.0-or-later

package askengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRestrictAcceptanceToProject: the model selects from the workspace's own
// checks. Anything it invented is dropped before the task row exists, so an
// arbitrary command never reaches the orchestrator's executor.
func TestRestrictAcceptanceToProject(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("test:\n\tgo test ./...\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := `{"scope":"x","acceptance":[{"id":"ok","run":"make test"},{"id":"invented","run":"rm -rf /tmp/anything"},{"id":"art","exists":["dist/app"]}]}`
	got := restrictAcceptanceToProject(spec, root)
	if strings.Contains(got, "rm -rf") {
		t.Fatalf("invented command survived: %s", got)
	}
	if !strings.Contains(got, "make test") || !strings.Contains(got, "dist/app") {
		t.Fatalf("declared and artifact checks should survive: %s", got)
	}

	// With nothing declared, a command contract cannot be substantiated and
	// the field is removed entirely.
	empty := t.TempDir()
	commandOnly := `{"scope":"x","acceptance":[{"id":"ok","run":"make test"}]}`
	got = restrictAcceptanceToProject(commandOnly, empty)
	if strings.Contains(got, "acceptance") {
		t.Fatalf("no candidates: command contract should be dropped: %s", got)
	}
	if !strings.Contains(got, `"scope":"x"`) {
		t.Fatalf("other spec fields must survive: %s", got)
	}
}
