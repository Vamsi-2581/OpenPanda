// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuildAcceptanceChecks covers the CLI's contract construction: explicit
// commands, project enumeration via "auto", duplicate collapse, and the
// refusals (shell syntax, nothing to infer).
func TestBuildAcceptanceChecks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("build:\n\tgo build ./...\ntest:\n\tgo test ./...\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := buildAcceptanceChecks([]string{"make test"}, root)
	if err != nil {
		t.Fatalf("explicit: %v", err)
	}
	if len(checks) != 1 || checks[0].Run != "make test" || checks[0].ID != "cli-1" {
		t.Fatalf("explicit checks = %+v", checks)
	}

	checks, err = buildAcceptanceChecks([]string{"make test", "make test"}, root)
	if err != nil || len(checks) != 1 {
		t.Fatalf("duplicates should collapse: %+v err=%v", checks, err)
	}

	checks, err = buildAcceptanceChecks([]string{"auto"}, root)
	if err != nil {
		t.Fatalf("auto: %v", err)
	}
	got := map[string]bool{}
	for _, c := range checks {
		got[c.Run] = true
	}
	if !got["make build"] || !got["make test"] {
		t.Fatalf("auto checks = %+v, want the Makefile targets", checks)
	}

	if _, err := buildAcceptanceChecks([]string{"make test && rm -rf /"}, root); err == nil {
		t.Fatal("shell syntax must be refused")
	}
	if _, err := buildAcceptanceChecks([]string{"auto"}, t.TempDir()); err == nil {
		t.Fatal("auto with nothing to infer must say so")
	}
}
