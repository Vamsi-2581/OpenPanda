// SPDX-License-Identifier: AGPL-3.0-or-later

package contract

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSplitCommand(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"make test", []string{"make", "test"}},
		{"go test ./...", []string{"go", "test", "./..."}},
		{`npm run "test:unit"`, []string{"npm", "run", "test:unit"}},
		{`sh -c 'echo hi'`, []string{"sh", "-c", "echo hi"}},
		{`a\ b c`, []string{"a b", "c"}},
		{`C:\work\bin --flag`, []string{`C:\work\bin`, "--flag"}},
	}
	for _, tc := range cases {
		got, err := SplitCommand(tc.in)
		if err != nil {
			t.Errorf("SplitCommand(%q) error: %v", tc.in, err)
			continue
		}
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Errorf("SplitCommand(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}

	for _, bad := range []string{
		"make test && rm -rf /",
		"echo hi | cat",
		"curl $(whoami)",
		"a; b",
		"echo `id`",
		"echo 'unterminated",
		"",
		"   ",
	} {
		if _, err := SplitCommand(bad); err == nil {
			t.Errorf("SplitCommand(%q) accepted shell syntax", bad)
		}
	}
}

func TestValidate(t *testing.T) {
	checks, err := Validate([]Check{
		{Run: "make test"},
		{ID: "art", Exists: []string{"dist/app"}},
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if checks[0].ID != "check-1" || checks[0].EffectiveKind() != KindCommand {
		t.Fatalf("first check = %+v, want auto id and command kind", checks[0])
	}
	if checks[1].EffectiveKind() != KindArtifact {
		t.Fatalf("artifact kind = %q", checks[1].EffectiveKind())
	}

	bads := [][]Check{
		{{ID: "a", Run: "make test"}, {ID: "a", Run: "go test ./..."}}, // duplicate
		{{Run: ""}},               // empty command
		{{Run: "sh -c 'x' && y"}}, // shell
		{{ID: "x", Kind: "visual", Run: "make test"}},  // unknown kind
		{{ID: "x", Exists: []string{"../etc/passwd"}}}, // escape
		{{ID: "x", Exists: []string{"/etc/passwd"}}},   // absolute
		{{ID: "x", Run: "false", Expect: Expect{Exit: intp(200)}}},
	}
	for i, bad := range bads {
		if _, err := Validate(bad); err == nil {
			t.Errorf("case %d: Validate accepted %+v", i, bad)
		}
	}

	var many []Check
	for i := 0; i < MaxChecks+1; i++ {
		many = append(many, Check{Run: "true"})
	}
	if _, err := Validate(many); err == nil {
		t.Fatal("Validate accepted too many checks")
	}
}

func intp(v int) *int { return &v }

func TestEnumerate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Makefile", "build:\n\tgo build ./...\ntest:\n\tgo test ./...\n.Phony: x\nclean:\n\trm -rf bin\n")
	writeFile(t, root, "package.json", `{"scripts":{"test":"vitest","lint":"eslint .","pretest":"x","deploy":"ship"}}`)
	writeFile(t, root, "yarn.lock", "")
	writeFile(t, root, "go.mod", "module example.com/x\n")
	writeFile(t, root, ".github/workflows/ci.yml", "jobs:\n  test:\n    steps:\n      - run: go test ./...\n      - run: cargo test --all\n      - run: echo ${{ secrets.X }}\n      - run: ./deploy.sh\n")

	cands := Enumerate(root)
	byID := map[string]Candidate{}
	var ciFound bool
	for _, c := range cands {
		byID[c.ID] = c
		if c.Source == "CI workflow" {
			ciFound = true
		}
	}
	for _, want := range []string{"make:build", "make:test", "pkg:lint", "pkg:test", "go:test", "go:vet", "go:build"} {
		if _, ok := byID[want]; !ok {
			t.Errorf("missing candidate %q in %+v", want, cands)
		}
	}
	if !ciFound {
		t.Errorf("CI workflow step was not enumerated: %+v", cands)
	}
	if _, ok := byID["pkg:pretest"]; ok {
		t.Error("pre/post scripts must not be offered")
	}
	if _, ok := byID["pkg:deploy"]; ok {
		t.Error("a deploy script is not an acceptance check")
	}
	if _, ok := byID["make:clean"]; ok {
		t.Error("a clean target is not an acceptance check")
	}
	for _, c := range cands {
		if strings.Contains(c.Run, "${{") || strings.Contains(c.Run, "deploy") {
			t.Errorf("unsafe CI step leaked in: %+v", c)
		}
		if c.Source == "package.json" && !strings.HasPrefix(c.Run, "yarn run ") {
			t.Errorf("package manager not detected: %+v", c)
		}
	}
}

func TestRunnerAndEvaluate(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dist/app", "x")
	checks, err := Validate([]Check{
		{ID: "ok", Run: "make test"},
		{ID: "art", Exists: []string{"dist/app"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(_ context.Context, _ string, argv []string) ExecResult {
		if argv[0] == "make" {
			return ExecResult{Stdout: "ok", Duration: time.Millisecond}
		}
		return ExecResult{ExitCode: 2, Stderr: "boom", Duration: time.Millisecond}
	}
	r := Runner{Dir: dir, Exec: exec}
	base := r.Run(context.Background(), "baseline", checks)
	if got := statusOf(base, "ok"); got != StatusPass {
		t.Fatalf("baseline ok = %q", got)
	}
	if v := Evaluate(checks, base, base); v.Status != VerdictVerified {
		t.Fatalf("verdict = %+v, want verified", v)
	}

	failExec := func(_ context.Context, _ string, argv []string) ExecResult {
		if argv[0] == "make" {
			return ExecResult{ExitCode: 1, Stderr: "assertion failed"}
		}
		return ExecResult{ExitCode: 0}
	}
	final := (&Runner{Dir: dir, Exec: failExec}).Run(context.Background(), "final", checks)
	v := Evaluate(checks, base, final)
	if v.Status != VerdictFailed {
		t.Fatalf("verdict = %+v, want failed", v)
	}
	if len(v.Regressions) != 1 || v.Regressions[0] != "ok" {
		t.Fatalf("regressions = %v, want [ok]", v.Regressions)
	}
	if len(v.Passed) != 1 || v.Passed[0] != "art" {
		t.Fatalf("passed = %v, want [art]", v.Passed)
	}

	// A check that was already failing is named, not excused.
	pre := []Result{{Phase: "baseline", CheckID: "ok", Status: StatusFail}}
	v = Evaluate(checks, pre, final)
	if len(v.PreExisting) != 1 || v.PreExisting[0] != "ok" {
		t.Fatalf("pre-existing = %v", v.PreExisting)
	}
	if v.Status != VerdictFailed {
		t.Fatalf("a still-failing check must not verify: %+v", v)
	}
}

func TestRunnerStdoutContainsAndFlaky(t *testing.T) {
	checks, _ := Validate([]Check{{ID: "probe", Run: "curl -fsS localhost:8080/health", Expect: Expect{StdoutContains: "ok"}}})
	calls := 0
	exec := func(context.Context, string, []string) ExecResult {
		calls++
		if calls == 1 {
			return ExecResult{ExitCode: 1, Stderr: "connection refused"}
		}
		return ExecResult{Stdout: "ok"}
	}
	r := Runner{Dir: t.TempDir(), Exec: exec, RetryFlaky: true}
	res := r.Run(context.Background(), "final", checks)
	if res[0].Status != StatusFlaky {
		t.Fatalf("status = %q, want flaky (reason %q)", res[0].Status, res[0].Reason)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want one retry", calls)
	}
}

func TestRunnerSkipsPastBudget(t *testing.T) {
	checks, _ := Validate([]Check{{ID: "a", Run: "true"}, {ID: "b", Run: "true"}})
	now := time.Now()
	clock := func() time.Time { return now }
	exec := func(context.Context, string, []string) ExecResult {
		now = now.Add(time.Minute)
		return ExecResult{ExitCode: 0}
	}
	r := Runner{Dir: t.TempDir(), Exec: exec, Now: clock, Budget: 30 * time.Second}
	res := r.Run(context.Background(), "final", checks)
	if res[0].Status != StatusPass {
		t.Fatalf("first = %+v", res[0])
	}
	if res[1].Status != StatusSkipped {
		t.Fatalf("second = %+v, want skipped", res[1])
	}
	v := Evaluate(checks, nil, res)
	if v.Status != VerdictPartial {
		t.Fatalf("verdict = %q, want partial", v.Status)
	}
}

func TestRunnerLaunchFailureIsError(t *testing.T) {
	checks, _ := Validate([]Check{{ID: "x", Run: "missing-binary --flag"}})
	exec := func(context.Context, string, []string) ExecResult {
		return ExecResult{Err: errors.New("exec: not found"), ExitCode: 127}
	}
	res := (&Runner{Dir: t.TempDir(), Exec: exec}).Run(context.Background(), "baseline", checks)
	if res[0].Status != StatusError {
		t.Fatalf("status = %q, want error", res[0].Status)
	}
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func statusOf(results []Result, id string) Status {
	for _, r := range results {
		if r.CheckID == id {
			return r.Status
		}
	}
	return ""
}
