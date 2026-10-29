// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !lite

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain keeps the whole package — the test process AND every panda child
// process a test spawns — out of the operator's real data directory. Several
// code paths resolve their defaults from os.UserConfigDir() rather than from
// the config handed in: the node identity lock, the ability card the engine
// materializes when the config names none, and the storage bootstrap that
// follows it. A per-test temp config cannot cover those, and a subprocess
// inherits the real HOME no matter what the parent test did.
//
// GOCACHE and GOPATH are pinned BEFORE HOME moves. Tests that shell out to
// `go build` (the CLI regression suite) would otherwise see an empty build and
// module cache under the temp home, and the package would go from seconds to
// minutes.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "panda-cmd-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmd/panda tests: temp home: %v\n", err)
		os.Exit(1)
	}

	os.Setenv("GOCACHE", goEnv("GOCACHE"))
	os.Setenv("GOPATH", goEnv("GOPATH"))
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	os.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	os.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	os.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))

	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// goEnv returns the current value of a go env key. The environment wins when
// it already carries one; otherwise ask the toolchain, before HOME changes.
func goEnv(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
