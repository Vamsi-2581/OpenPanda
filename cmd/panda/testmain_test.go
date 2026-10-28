// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain moves the per-user directories into a throwaway tree for the whole
// package. Several CLI paths resolve defaults through os.UserConfigDir — the
// capability card, the node-identity lock, and config.Default()'s storage —
// so a test that passes only some paths would leave a database, card or lock
// files in the developer's real ~/Library/Application Support/openpanda.
func TestMain(m *testing.M) {
	// Tests that shell out to `go build` (TestCLIVersion) would otherwise get
	// a cold build and module cache under the temp home.
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
	home, err := os.MkdirTemp("", "panda-cli-test-home-")
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
