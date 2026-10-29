// SPDX-License-Identifier: AGPL-3.0-or-later

package askengine

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain moves the per-user directories into a throwaway tree for the whole
// package. New() resolves an unset capability-card path through
// os.UserConfigDir and takes a node-identity lock there, so a test that passes
// only storage paths would otherwise write capabilities.yaml and lock files
// into the developer's real ~/Library/Application Support/openpanda.
func TestMain(m *testing.M) {
	// Keep the Go caches outside the temp home; a cold module cache would turn
	// any test that shells out to `go` into a multi-minute stall.
	if os.Getenv("GOCACHE") == "" {
		if dir, err := os.UserCacheDir(); err == nil {
			os.Setenv("GOCACHE", filepath.Join(dir, "go-build"))
		}
	}
	if os.Getenv("GOPATH") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			os.Setenv("GOPATH", filepath.Join(home, "go"))
		}
	}
	home, err := os.MkdirTemp("", "askengine-test-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", home)
	os.Setenv("APPDATA", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
