// SPDX-License-Identifier: AGPL-3.0-or-later

package askengine

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Xustalis/OpenPanda/internal/memory"
)

// TestNoteDailyObservation pins the trusted-writer contract the Dreaming
// engine depends on: an agent-authored digest lands in the day's log WITHOUT
// the [ext] taint marker, so light() counts it as an agent observation and a
// recurring digest can actually clear the Deep gate.
func TestNoteDailyObservation(t *testing.T) {
	root := t.TempDir()
	hermes := memory.NewHermes(root)
	e := &Engine{
		daily:  memory.NewDaily(hermes.WarmDir()),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	e.noteDailyObservation("任务总结：巡检脚本构建成功")
	e.noteDailyObservation("   ") // blank digests are dropped
	e.noteDailyObservation("")

	matches, err := filepath.Glob(filepath.Join(hermes.WarmDir(), "*.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("daily log not written: %v %v", matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("daily log not written: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "任务总结：巡检脚本构建成功") {
		t.Fatalf("digest missing from daily log: %q", got)
	}
	if strings.Contains(got, memory.ExternalMarker) {
		t.Fatalf("agent observation must be trusted (no %s): %q", memory.ExternalMarker, got)
	}
	if n := strings.Count(strings.TrimSpace(got), "\n") + 1; n != 1 {
		t.Fatalf("daily lines = %d, want 1 (blank digests dropped): %q", n, got)
	}

	// A nil-daily engine (hand-built test engines) must not panic.
	(&Engine{}).noteDailyObservation("x")
}
