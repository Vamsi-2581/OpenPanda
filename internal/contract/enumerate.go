// SPDX-License-Identifier: AGPL-3.0-or-later

package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Candidate is one verification entry point the project already declares. The
// model and the user select from this list; they never invent a command, which
// is what keeps the acceptance layer from becoming a new attack surface.
type Candidate struct {
	ID     string `json:"id"`
	Run    string `json:"run"`
	Source string `json:"source"`
}

const (
	// maxCandidateBytes bounds a file the enumerator reads. A generated 40 MB
	// Makefile is not a project convention worth parsing.
	maxCandidateBytes = 512 << 10
	// maxCandidates keeps a contract-sized list: more entries turn selection
	// into a menu nobody reads, and every selected check costs a real run.
	// It equals the contract's own ceiling so `--accept auto` — which selects
	// everything enumerated — can never overflow Validate on a busy project.
	maxCandidates = MaxChecks
)

// interestingTarget matches build/test target names. It is deliberately
// narrow: a Makefile's `deploy` or `clean` target is not an acceptance check,
// and running it during verification could have side effects.
var interestingTarget = regexp.MustCompile(`(?i)(^|[-_])(test|tests|check|lint|vet|fmt|build|verify|ci|e2e|unit)([-_]|$)`)

// makeTargetRE matches a target declaration up to its colon. Go's regexp has
// no lookahead, so the `:=` assignment check is done on the byte after the
// match instead.
var makeTargetRE = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*)[ \t]*:`)

// interestingScript matches package.json script names worth running.
var interestingScript = regexp.MustCompile(`(?i)(test|spec|lint|typecheck|check|build|e2e|ci)`)

// ciTools are the programs a CI `run:` line may start with. The program alone
// is not enough to make a step safe to replay: `npm publish` and `make
// release` share their first word with `npm test` and `make test`, and both
// have side effects nobody asked to repeat during verification. ciAllowed
// therefore also checks the subcommand.
var ciTools = map[string]bool{
	"make": true, "npm": true, "pnpm": true, "yarn": true, "bun": true,
	"go": true, "cargo": true, "pytest": true, "python": true, "python3": true,
	"dotnet": true, "mvn": true, "gradle": true, "./gradlew": true, "./mvnw": true,
}

// ciPositionals returns the non-flag words of an argument list. Values that
// belong to a flag are not distinguished (a bare `--max 3` leaves "3"); the
// allowlist below only ever inspects the first one or two words, and a stray
// value there is a reason to skip the step, not to run it.
func ciPositionals(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		out = append(out, a)
	}
	return out
}

// ciAllowed reports whether a CI step is a verification entry point rather
// than a release, publish or deploy step that happens to use the same tool.
func ciAllowed(argv []string) bool {
	if len(argv) == 0 || !ciTools[argv[0]] {
		return false
	}
	switch argv[0] {
	case "make":
		pos := ciPositionals(argv[1:])
		return len(pos) > 0 && interestingTarget.MatchString(pos[0])
	case "npm", "pnpm", "yarn", "bun":
		pos := ciPositionals(argv[1:])
		if len(pos) == 0 {
			return false
		}
		if pos[0] == "test" {
			return true
		}
		// `run <script>` is the only other shape; `publish`, `install` and
		// friends are not verification.
		return pos[0] == "run" && len(pos) > 1 && interestingScript.MatchString(pos[1])
	case "go":
		pos := ciPositionals(argv[1:])
		if len(pos) == 0 {
			return false
		}
		switch pos[0] {
		case "test", "vet", "build":
			return true
		}
		return false
	case "cargo":
		pos := ciPositionals(argv[1:])
		if len(pos) == 0 {
			return false
		}
		switch pos[0] {
		case "test", "build", "check", "clippy", "fmt":
			return true
		}
		return false
	case "python", "python3":
		// Only `-m pytest`: running a project script by name would execute
		// arbitrary code the step never declared as a test.
		return len(argv) >= 3 && argv[1] == "-m" && argv[2] == "pytest"
	case "pytest":
		return true
	case "dotnet":
		pos := ciPositionals(argv[1:])
		return len(pos) > 0 && (pos[0] == "test" || pos[0] == "build")
	case "mvn", "./mvnw":
		pos := ciPositionals(argv[1:])
		return len(pos) > 0 && (pos[0] == "test" || pos[0] == "verify")
	case "gradle", "./gradlew":
		pos := ciPositionals(argv[1:])
		return len(pos) > 0 && (pos[0] == "test" || pos[0] == "build" || pos[0] == "check")
	}
	return false
}

// Enumerate inspects root for verification entry points the project already
// declares. It reads fixed, small files and never walks the tree, so it is
// cheap enough to run before every ask.
func Enumerate(root string) []Candidate {
	if root == "" {
		return nil
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil
	}
	var out []Candidate
	seen := map[string]bool{}
	add := func(id, run, source string) {
		run = strings.TrimSpace(run)
		if run == "" || seen[run] || len(out) >= maxCandidates {
			return
		}
		if _, err := SplitCommand(run); err != nil {
			return
		}
		seen[run] = true
		out = append(out, Candidate{ID: id, Run: run, Source: source})
	}

	for _, target := range makefileTargets(filepath.Join(root, "Makefile"), filepath.Join(root, "makefile"), filepath.Join(root, "GNUmakefile")) {
		add("make:"+target, "make "+target, "Makefile")
	}
	for _, script := range packageScripts(root) {
		add("pkg:"+script, packageManager(root)+" run "+script, "package.json")
	}
	if exists(filepath.Join(root, "go.mod")) {
		add("go:test", "go test ./...", "go.mod")
		add("go:vet", "go vet ./...", "go.mod")
		add("go:build", "go build ./...", "go.mod")
	}
	if exists(filepath.Join(root, "Cargo.toml")) {
		add("cargo:test", "cargo test", "Cargo.toml")
		add("cargo:build", "cargo build", "Cargo.toml")
	}
	if hasPytest(root) {
		add("py:pytest", "pytest -q", "python project")
	}
	for _, run := range ciRuns(root) {
		add("ci:"+sanitizeID(run), run, "CI workflow")
	}
	return out
}

// makefileTargets returns the interesting targets declared in the first
// Makefile that exists. Targets keep file order so the list is stable.
func makefileTargets(paths ...string) []string {
	var lines []string
	for _, p := range paths {
		if blob, ok := readCapped(p); ok {
			lines = strings.Split(blob, "\n")
			break
		}
	}
	var targets []string
	seen := map[string]bool{}
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ") {
			continue // recipe line
		}
		m := makeTargetRE.FindStringSubmatchIndex(line)
		if m == nil {
			continue
		}
		// m[1] is the end of the whole match: a `:=` assignment must not be
		// read as a target named "X".
		if m[1] < len(line) && line[m[1]] == '=' {
			continue
		}
		name := line[m[2]:m[3]]
		if !interestingTarget.MatchString(name) || seen[name] {
			continue
		}
		seen[name] = true
		targets = append(targets, name)
	}
	return targets
}

// packageScripts returns the verification-ish scripts a package.json declares.
func packageScripts(root string) []string {
	blob, ok := readCapped(filepath.Join(root, "package.json"))
	if !ok {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal([]byte(blob), &pkg) != nil {
		return nil
	}
	keys := make([]string, 0, len(pkg.Scripts))
	for k := range pkg.Scripts {
		lower := strings.ToLower(k)
		if strings.HasPrefix(lower, "pre") || strings.HasPrefix(lower, "post") {
			continue
		}
		if interestingScript.MatchString(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// packageManager picks the runner from the lockfile the project actually has.
func packageManager(root string) string {
	switch {
	case exists(filepath.Join(root, "pnpm-lock.yaml")):
		return "pnpm"
	case exists(filepath.Join(root, "yarn.lock")):
		return "yarn"
	case exists(filepath.Join(root, "bun.lock")), exists(filepath.Join(root, "bun.lockb")):
		return "bun"
	default:
		return "npm"
	}
}

// hasPytest reports whether the project looks pytest-shaped. It checks config
// files and the conventional tests directory, never a recursive scan.
func hasPytest(root string) bool {
	if exists(filepath.Join(root, "pytest.ini")) || exists(filepath.Join(root, "tox.ini")) {
		return true
	}
	if blob, ok := readCapped(filepath.Join(root, "pyproject.toml")); ok && strings.Contains(blob, "pytest") {
		return true
	}
	if blob, ok := readCapped(filepath.Join(root, "setup.cfg")); ok && strings.Contains(blob, "pytest") {
		return true
	}
	return false
}

// ciRuns extracts simple `run:` steps from GitHub workflows. Only direct
// program calls on known tools survive: a step that needs a shell, secrets, or
// a conditional would not mean the same thing when replayer here.
func ciRuns(root string) []string {
	dir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var out []string
	seen := map[string]bool{}
	for _, name := range names {
		blob, ok := readCapped(filepath.Join(dir, name))
		if !ok {
			continue
		}
		for _, line := range strings.Split(blob, "\n") {
			line = strings.TrimRight(line, "\r")
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "run:") && !strings.HasPrefix(trimmed, "- run:") {
				continue
			}
			cmd := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "run:"))
			cmd = strings.TrimSpace(strings.Trim(cmd, `"'`))
			if cmd == "" || strings.Contains(cmd, "${{") || seen[cmd] {
				continue
			}
			argv, err := SplitCommand(cmd)
			if err != nil || !ciAllowed(argv) {
				continue
			}
			seen[cmd] = true
			out = append(out, cmd)
		}
	}
	return out
}

// sanitizeID makes a stable, short id fragment from a command.
func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '/' || r == '.':
			b.WriteRune('-')
		}
		if b.Len() >= 40 {
			break
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		return "step"
	}
	return id
}

// readCapped reads a file no larger than maxCandidateBytes.
func readCapped(path string) (string, bool) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() || st.Size() > maxCandidateBytes {
		return "", false
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(blob), true
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
