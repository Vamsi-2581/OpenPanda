// SPDX-License-Identifier: AGPL-3.0-or-later

package panel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Xustalis/OpenPanda/internal/config"
)

// TestMain points the per-user directories at a throwaway tree for the whole
// package. Some tests reach os.UserConfigDir through the node-identity lock
// without going through isolatedConfig; without this they leave lock files
// (and, worse, a capability card) in the developer's real user data.
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
	home, err := os.MkdirTemp("", "panel-test-home-")
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

// isolateUserDirs moves the per-user dirs os.UserConfigDir resolves — and
// with them nodeidentity's lock root — under a temp dir. The identity lock
// ignores the config an engine is handed, so a test that skips this leaves
// lock files in the developer's real ~/Library/Application Support/openpanda.
func isolateUserDirs(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("APPDATA", tmp)
}

// isolatedConfig returns config.Default() with every path an engine touches
// moved under a temp dir. Default() alone points the store, memory, skills and
// capability card at the developer's real user data, and the node-identity
// lock resolves through os.UserConfigDir regardless of the config it is
// handed — a panel test that builds an engine would otherwise create a
// database and capabilities.yaml in ~/Library/Application Support/openpanda.
func isolatedConfig(t *testing.T) *config.Config {
	t.Helper()
	isolateUserDirs(t)
	tmp := t.TempDir()
	cfg := config.Default()
	cfg.Node.CardPath = filepath.Join(tmp, "capabilities.yaml")
	cfg.Storage.DBPath = filepath.Join(tmp, "engine.db")
	cfg.Storage.ContextPath = filepath.Join(tmp, "context")
	cfg.Storage.MemoryPath = filepath.Join(tmp, "memory")
	cfg.Storage.ProjectsPath = filepath.Join(tmp, "projects")
	cfg.Storage.SkillsPath = filepath.Join(tmp, "skills")
	cfg.Storage.WorkPath = tmp
	cfg.Storage.ArtifactPath = filepath.Join(tmp, "artifacts")
	return cfg
}
