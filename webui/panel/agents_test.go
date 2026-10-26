// SPDX-License-Identifier: AGPL-3.0-or-later

package panel

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeCLI writes a shell script that appends a line to a counter file and
// prints a version banner, so a test can tell how many times it was spawned.
// Windows is skipped: exec cannot run a .cmd fixture without a shell, and the
// cache logic itself is platform-independent.
func fakeCLI(t *testing.T, banner string, exitCode int) (script, counter string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture")
	}
	dir := t.TempDir()
	script = filepath.Join(dir, "fakecli")
	counter = filepath.Join(dir, "spawns")
	body := "#!/bin/sh\n" +
		"echo run >> '" + counter + "'\n" +
		"echo '" + banner + "'\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return script, counter
}

func spawnCount(t *testing.T, counter string) int {
	t.Helper()
	data, err := os.ReadFile(counter)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read counter: %v", err)
	}
	return len(strings.Fields(string(data)))
}

// TestProbeVersionCaches pins the reason the cache exists: the roster endpoint
// is fetched by four views, and each miss costs one CLI start per installed
// agent. Without the cache that is a visible stall on every console open.
func TestProbeVersionCaches(t *testing.T) {
	script, counter := fakeCLI(t, "fake 1.0.0", 0)

	version, err := probeVersion(script, false)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if version != "fake 1.0.0" {
		t.Fatalf("version = %q", version)
	}
	if _, err := probeVersion(script, false); err != nil {
		t.Fatalf("cached probe: %v", err)
	}
	if got := spawnCount(t, counter); got != 1 {
		t.Fatalf("spawns = %d, want 1 (second call served from cache)", got)
	}
}

// TestProbeVersionForceRefreshes pins the test button's contract: a click must
// ask the CLI itself, or a fix the user just made still reports the old
// failure — and the refreshed answer must reach the roster.
func TestProbeVersionForceRefreshes(t *testing.T) {
	script, counter := fakeCLI(t, "fake 1.0.0", 0)
	if _, err := probeVersion(script, false); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if _, err := probeVersion(script, true); err != nil {
		t.Fatalf("forced probe: %v", err)
	}
	if got := spawnCount(t, counter); got != 2 {
		t.Fatalf("spawns = %d, want 2 (force bypasses the cache)", got)
	}
}

// TestProbeVersionExpires pins the TTL: an upgraded CLI must not report its old
// version forever. The entry is aged directly so the test does not wait.
func TestProbeVersionExpires(t *testing.T) {
	script, counter := fakeCLI(t, "fake 1.0.0", 0)
	if _, err := probeVersion(script, false); err != nil {
		t.Fatalf("probe: %v", err)
	}
	versionCacheMu.Lock()
	versionCache[script] = versionProbe{version: "stale", at: time.Now().Add(-2 * versionTTL)}
	versionCacheMu.Unlock()

	version, err := probeVersion(script, false)
	if err != nil {
		t.Fatalf("re-probe: %v", err)
	}
	if version != "fake 1.0.0" {
		t.Fatalf("version = %q, want a fresh probe", version)
	}
	if got := spawnCount(t, counter); got != 2 {
		t.Fatalf("spawns = %d, want 2 (expired entry re-probed)", got)
	}
}

// TestProbeVersionCachesFailure pins the negative case: a CLI that hangs or
// exits non-zero on --version is the most expensive one to ask twice, and the
// roster only needs to know it has nothing to show.
func TestProbeVersionCachesFailure(t *testing.T) {
	script, counter := fakeCLI(t, "boom", 3)
	if _, err := probeVersion(script, false); err == nil {
		t.Fatal("expected the fixture's non-zero exit to surface")
	}
	version, err := probeVersion(script, false)
	if err == nil {
		t.Fatal("cached failure lost its error")
	}
	if version != "boom" {
		// stderr carries it for CLIs that print the banner there and exit
		// non-zero; the fixture prints on stdout before failing.
		t.Fatalf("version = %q, want the banner even when the exit code is bad", version)
	}
	if got := spawnCount(t, counter); got != 1 {
		t.Fatalf("spawns = %d, want 1 (a failure is cached too)", got)
	}
}
