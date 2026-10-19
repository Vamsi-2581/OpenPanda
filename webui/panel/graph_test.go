// SPDX-License-Identifier: AGPL-3.0-or-later

package panel

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Xustalis/OpenPanda/internal/memory"
	"github.com/Xustalis/OpenPanda/internal/skills"
)

// graphNodeIDs flattens a graph response into a set for membership checks.
func graphNodeIDs(t *testing.T, out map[string]any) map[string]bool {
	t.Helper()
	raw, ok := out["nodes"].([]any)
	if !ok {
		t.Fatalf("nodes missing or wrong type: %T", out["nodes"])
	}
	ids := map[string]bool{}
	for _, n := range raw {
		m, ok := n.(map[string]any)
		if !ok {
			t.Fatalf("node entry wrong type: %T", n)
		}
		ids[m["id"].(string)] = true
	}
	return ids
}

func graphHasEdge(out map[string]any, from, to, kind string) bool {
	return graphEdgeWeight(out, from, to, kind) > 0
}

// graphEdgeWeight returns the edge's weight, 0 when the edge is absent (the
// backend only emits positive weights, so existence and weight coincide).
func graphEdgeWeight(out map[string]any, from, to, kind string) int {
	raw, _ := out["edges"].([]any)
	for _, e := range raw {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if m["from"] == from && m["to"] == to && m["kind"] == kind {
			w, _ := m["weight"].(float64)
			return int(w)
		}
	}
	return 0
}

// A fresh node still answers 200 with the core surfaces — sparse, not broken.
func TestMemoryGraphEmptyMemory(t *testing.T) {
	h, _ := newMemoryPanel(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, authedReq(http.MethodGet, "/api/memory/graph", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	ids := graphNodeIDs(t, out)
	for _, want := range []string{"core:user", "core:memory", "core:dreams", "daily"} {
		if !ids[want] {
			t.Errorf("missing core node %s in %+v", want, ids)
		}
	}
}

// Promotion tags and literal name mentions must become edges — the two
// honest edge kinds the view promises.
func TestMemoryGraphEdges(t *testing.T) {
	_, cfg := newMemoryPanel(t)
	hermes := memory.NewHermesWithLimits(cfg.Storage.MemoryPath, memory.Limits{})

	// MEMORY.md carries dreamer-promoted entries; one names a topic twice and
	// one points back at USER.md (a core-file mention is a reference edge too).
	if err := hermes.SaveMemory(memory.MemFile{Entries: []string{
		"team ships fridays " + memory.PromotionSourceTag,
		"deploys moved " + memory.PromotionSourceTag,
		"see work and the work calendar",
		"profile fields live in USER.md",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := hermes.SaveTopic("work", memory.MemFile{Entries: []string{"friday deploys"}}); err != nil {
		t.Fatal(err)
	}
	// A daily file gives the diary node real mass.
	dailyDir := hermes.WarmDir()
	if err := os.MkdirAll(dailyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dailyDir, "2026-01-02.md"), []byte("# diary\nshipped a thing"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A pending skill renders dimmed; status must round-trip. Rebuild the
	// panel with a skill store wired in.
	skillStore := skills.NewStore(t.TempDir())
	if err := skillStore.Save(&skills.Skill{Name: "deploy-check", Description: "verify deploys", Status: skills.StatusPending, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	// A skill whose description names a topic earns a skill→topic reference.
	if err := skillStore.Save(&skills.Skill{Name: "work-report", Description: "summarize the work log", Status: skills.StatusActive, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	h := New(Deps{
		Store:      newTestStore(t),
		Projects:   memory.NewProjectsWithLimits(cfg.Storage.ProjectsPath, memory.Limits{}),
		Cfg:        cfg,
		SkillStore: skillStore,
		StaticDir:  t.TempDir(),
		Token:      testToken,
	})

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, authedReq(http.MethodGet, "/api/memory/graph", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	ids := graphNodeIDs(t, out)
	for _, want := range []string{"topic:work", "skill:deploy-check", "daily"} {
		if !ids[want] {
			t.Errorf("missing node %s", want)
		}
	}
	// [from:日志] inside MEMORY.md is the diary feeding long-term memory —
	// two promoted entries make the edge twice as strong.
	if w := graphEdgeWeight(out, "daily", "core:memory", "promotion"); w != 2 {
		t.Errorf("promotion weight = %d, want 2: %v", w, out["edges"])
	}
	// MEMORY.md naming "work" twice is a reference edge with weight 2.
	if w := graphEdgeWeight(out, "core:memory", "topic:work", "reference"); w != 2 {
		t.Errorf("reference weight = %d, want 2: %v", w, out["edges"])
	}
	// A surface naming a core file is an honest reference edge.
	if !graphHasEdge(out, "core:memory", "core:user", "reference") {
		t.Errorf("reference edge core:memory→core:user missing: %v", out["edges"])
	}
	// A skill's description naming a topic links skill→topic.
	if !graphHasEdge(out, "skill:work-report", "topic:work", "reference") {
		t.Errorf("reference edge skill:work-report→topic:work missing: %v", out["edges"])
	}
}

// graphNode returns the node map with the given id, failing the test when the
// node is missing.
func graphNode(t *testing.T, out map[string]any, id string) map[string]any {
	t.Helper()
	raw, _ := out["nodes"].([]any)
	for _, n := range raw {
		if m, ok := n.(map[string]any); ok && m["id"] == id {
			return m
		}
	}
	t.Fatalf("node %s missing: %v", id, out["nodes"])
	return nil
}

// The knowledge layer: recurring terms become entity nodes with dated
// evidence, tied to the surfaces containing them by mention edges and to each
// other by co-occurrence edges. Names already carrying a node stay out.
func TestMemoryGraphEntities(t *testing.T) {
	_, cfg := newMemoryPanel(t)
	hermes := memory.NewHermesWithLimits(cfg.Storage.MemoryPath, memory.Limits{})
	if err := hermes.SaveTopic("work", memory.MemFile{Entries: []string{"friday deploys"}}); err != nil {
		t.Fatal(err)
	}
	dailyDir := hermes.WarmDir()
	if err := os.MkdirAll(dailyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	day := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dailyDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	day("2026-01-02.md", "巡护链路 只读排查\n巡护链路 只读排查\n巡护链路 只读排查\n")
	day("2026-01-03.md", "巡护链路 只读排查\ndog dog\n")
	h := New(Deps{
		Store:     newTestStore(t),
		Projects:  memory.NewProjectsWithLimits(cfg.Storage.ProjectsPath, memory.Limits{}),
		Cfg:       cfg,
		StaticDir: t.TempDir(),
		Token:     testToken,
	})

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, authedReq(http.MethodGet, "/api/memory/graph", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	ids := graphNodeIDs(t, out)
	for _, want := range []string{"entity:巡护链路", "entity:只读排查", "entity:dog"} {
		if !ids[want] {
			t.Errorf("missing entity node %s", want)
		}
	}
	if ids["entity:work"] {
		t.Errorf("a topic name must not duplicate into the knowledge layer")
	}
	n := graphNode(t, out, "entity:巡护链路")
	if n["layer"] != "entity" || n["kind"] != "entity" {
		t.Errorf("entity layer/kind = %v/%v, want entity/entity", n["layer"], n["kind"])
	}
	if w, _ := n["weight"].(float64); int(w) != 4 {
		t.Errorf("entity weight = %v, want 4", n["weight"])
	}
	if n["first_seen"] != "2026-01-02" || n["last_seen"] != "2026-01-03" {
		t.Errorf("entity dates = %v..%v, want 2026-01-02..2026-01-03", n["first_seen"], n["last_seen"])
	}
	// Mention: the diary contains the phrase four times across two days.
	if w := graphEdgeWeight(out, "daily", "entity:巡护链路", "mention"); w != 4 {
		t.Errorf("mention weight = %d, want 4", w)
	}
	// Co-occurrence: the two phrases share all four lines.
	if w := graphEdgeWeight(out, "entity:只读排查", "entity:巡护链路", "co"); w != 4 {
		t.Errorf("co weight = %d, want 4", w)
	}
	// The diary node carries the filename dates, not the file mtime.
	d := graphNode(t, out, "daily")
	if d["first_seen"] != "2026-01-02" || d["last_seen"] != "2026-01-03" {
		t.Errorf("daily dates = %v..%v, want 2026-01-02..2026-01-03", d["first_seen"], d["last_seen"])
	}
	// Surfaces carry the provenance layer flag.
	if m := graphNode(t, out, "core:memory"); m["layer"] != "file" {
		t.Errorf("core node layer = %v, want file", m["layer"])
	}
}

// The dream diary is dated by its own section headers, not the file mtime.
func TestMemoryGraphDreamDates(t *testing.T) {
	_, cfg := newMemoryPanel(t)
	content := "## 2026-01-01\n- scanned 1\n## 2026-01-04\n- promoted 2\n"
	if err := os.WriteFile(filepath.Join(cfg.Storage.MemoryPath, "DREAMS.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(Deps{
		Store:     newTestStore(t),
		Projects:  memory.NewProjectsWithLimits(cfg.Storage.ProjectsPath, memory.Limits{}),
		Cfg:       cfg,
		StaticDir: t.TempDir(),
		Token:     testToken,
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, authedReq(http.MethodGet, "/api/memory/graph", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	n := graphNode(t, out, "core:dreams")
	if n["first_seen"] != "2026-01-01" || n["last_seen"] != "2026-01-04" {
		t.Errorf("dream dates = %v..%v, want 2026-01-01..2026-01-04", n["first_seen"], n["last_seen"])
	}
}

// The response must be byte-identical across requests — edges dedup through a
// map, so any ordering leak would surface as flaky diffs in the UI and tests.
func TestMemoryGraphDeterministic(t *testing.T) {
	h, cfg := newMemoryPanel(t)
	hermes := memory.NewHermesWithLimits(cfg.Storage.MemoryPath, memory.Limits{})
	if err := hermes.SaveMemory(memory.MemFile{Entries: []string{
		"alpha " + memory.PromotionSourceTag,
		"mentions beta-topic",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := hermes.SaveTopic("beta-topic", memory.MemFile{Entries: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	// Entity-rich diary text exercises the knowledge layer's maps (terms,
	// mentions, co-occurrence) under the same byte-identity check.
	dailyDir := hermes.WarmDir()
	if err := os.MkdirAll(dailyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dailyDir, "2026-01-02.md"), []byte("巡检链路 部署巡检\n巡检链路 部署巡检\ndog dog\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var first []byte
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, authedReq(http.MethodGet, "/api/memory/graph", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if first == nil {
			first = append([]byte(nil), rec.Body.Bytes()...)
			continue
		}
		if !bytes.Equal(first, rec.Body.Bytes()) {
			t.Fatalf("request %d returned different bytes:\n%s\n%s", i, first, rec.Body.Bytes())
		}
	}
}

// mentionsName is the mention scanner: word-boundary-aware for ASCII so
// "go" in "golang" does not link; plain substring for CJK.
func TestMentionsName(t *testing.T) {
	cases := []struct {
		content, name string
		want          bool
	}{
		{"the work calendar", "work", true},
		{"see golang docs", "go", false},
		{"uses go.", "go", true},
		{"go is great", "go", true},
		{"the deploy-check runs", "deploy-check", true},
		{"deploy-checks daily", "deploy-check", false},
		{"提到工作主题", "工作", true},
	}
	for _, c := range cases {
		if got := mentionsName(c.content, c.name); got != c.want {
			t.Errorf("mentionsName(%q, %q) = %v, want %v", c.content, c.name, got, c.want)
		}
	}
	if mentionable("ab") || !mentionable("abc") {
		t.Error("mentionable: names under 3 runes must be skipped")
	}
}
