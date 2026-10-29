// SPDX-License-Identifier: AGPL-3.0-or-later

// Package contract turns a task's stated success criteria into checks the
// orchestrator runs itself. An agent's own "done" is a claim; a contract is
// evidence.
//
// The package is deliberately dependency-free: it parses, enumerates, executes
// and evaluates, and the caller supplies the process launcher. That keeps the
// rules testable without a daemon and lets the core run checks through the same
// sandboxed executor native commands use.
package contract

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// Check kinds. A command runs a program; an artifact asserts that paths exist.
const (
	KindCommand  = "command"
	KindArtifact = "artifact"
)

// Limits keep a model-authored contract from becoming a workload of its own.
const (
	MaxChecks    = 8
	MaxArgs      = 64
	MaxRunLength = 1024
)

// Check is one acceptance criterion. The zero Expect means "exit 0".
type Check struct {
	ID     string   `json:"id"`
	Kind   string   `json:"kind,omitempty"`
	Run    string   `json:"run,omitempty"`
	Exists []string `json:"exists,omitempty"`
	Expect Expect   `json:"expect,omitempty"`
}

// Expect describes what a successful run looks like. Exit nil means 0.
type Expect struct {
	Exit           *int   `json:"exit,omitempty"`
	StdoutContains string `json:"stdout_contains,omitempty"`
}

// EffectiveKind normalizes the kind field: an empty kind is an artifact check
// when it lists paths and a command check otherwise.
func (c Check) EffectiveKind() string {
	if c.Kind != "" {
		return c.Kind
	}
	if len(c.Exists) > 0 {
		return KindArtifact
	}
	return KindCommand
}

// ExpectedExit is the exit code the check requires.
func (c Check) ExpectedExit() int {
	if c.Expect.Exit != nil {
		return *c.Expect.Exit
	}
	return 0
}

// Validate normalizes and checks a set of acceptance criteria. IDs are assigned
// when empty and must be unique when present. Commands must parse without a
// shell, artifact paths must stay inside the workspace.
func Validate(checks []Check) ([]Check, error) {
	if len(checks) > MaxChecks {
		return nil, fmt.Errorf("contract: %d checks, limit is %d", len(checks), MaxChecks)
	}
	out := make([]Check, 0, len(checks))
	seen := make(map[string]bool, len(checks))
	for i, c := range checks {
		c.ID = strings.TrimSpace(c.ID)
		if c.ID == "" {
			c.ID = fmt.Sprintf("check-%d", i+1)
		}
		if len(c.ID) > 64 {
			return nil, fmt.Errorf("contract: check id %q is too long", c.ID)
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("contract: duplicate check id %q", c.ID)
		}
		seen[c.ID] = true
		switch c.EffectiveKind() {
		case KindCommand:
			c.Run = strings.TrimSpace(c.Run)
			if c.Run == "" {
				return nil, fmt.Errorf("contract: check %q has no command", c.ID)
			}
			if len(c.Run) > MaxRunLength {
				return nil, fmt.Errorf("contract: check %q command is too long", c.ID)
			}
			if _, err := SplitCommand(c.Run); err != nil {
				return nil, fmt.Errorf("contract: check %q: %w", c.ID, err)
			}
			c.Exists = nil
		case KindArtifact:
			if len(c.Exists) == 0 {
				return nil, fmt.Errorf("contract: check %q lists no paths", c.ID)
			}
			for _, p := range c.Exists {
				if err := validatePath(p); err != nil {
					return nil, fmt.Errorf("contract: check %q: %w", c.ID, err)
				}
			}
			c.Run = ""
		default:
			return nil, fmt.Errorf("contract: check %q has unknown kind %q", c.ID, c.Kind)
		}
		if c.Expect.Exit != nil && (*c.Expect.Exit < 0 || *c.Expect.Exit > 125) {
			return nil, fmt.Errorf("contract: check %q expects exit %d, valid range is 0-125", c.ID, *c.Expect.Exit)
		}
		out = append(out, c)
	}
	return out, nil
}

// validatePath accepts a workspace-relative path that cannot escape the
// workspace. Absolute paths and parent traversal are refused: an artifact
// check must not be able to assert on files outside the run's tree.
func validatePath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return errors.New("empty artifact path")
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return fmt.Errorf("artifact path %q must be workspace-relative", p)
	}
	clean := filepath.Clean(p)
	if clean == "." {
		// "." is the workspace itself, which always exists: an artifact check
		// on it passes without asserting anything, so it is a self-certifying
		// contract rather than evidence.
		return errors.New(`artifact path "." asserts nothing; name a file or directory`)
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("artifact path %q escapes the workspace", p)
	}
	return nil
}

// shellMeta are the characters SplitCommand refuses outside quotes. The
// acceptance runner executes argv directly; anything that expects a shell
// would silently do something different (or nothing), so it is rejected at
// parse time instead of surprising the user at verification time.
const shellMeta = ";|&<>$`\n\r()"

// SplitCommand splits a command line into argv without invoking a shell.
// Single and double quotes group words; a backslash escapes the next rune
// outside single quotes when it precedes a quote, a backslash or whitespace.
// Any shell metacharacter outside quotes is an error.
func SplitCommand(line string) ([]string, error) {
	runes := []rune(line)
	var args []string
	var cur strings.Builder
	inSingle, inDouble := false, false
	hasToken := false
	flush := func() {
		if hasToken {
			args = append(args, cur.String())
			cur.Reset()
			hasToken = false
		}
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case inSingle:
			if r == '\'' {
				inSingle = false
				continue
			}
			cur.WriteRune(r)
			hasToken = true
		case inDouble:
			if r == '"' {
				inDouble = false
				continue
			}
			if r == '\\' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\') {
				i++
				cur.WriteRune(runes[i])
				hasToken = true
				continue
			}
			cur.WriteRune(r)
			hasToken = true
		default:
			switch {
			case r == '\'':
				inSingle = true
				hasToken = true
			case r == '"':
				inDouble = true
				hasToken = true
			case r == '\\' && i+1 < len(runes) && needsEscape(runes[i+1]):
				i++
				cur.WriteRune(runes[i])
				hasToken = true
			case unicode.IsSpace(r):
				flush()
			case strings.ContainsRune(shellMeta, r):
				return nil, fmt.Errorf("shell syntax %q is not allowed in an acceptance command; use a direct program call", string(r))
			default:
				cur.WriteRune(r)
				hasToken = true
			}
		}
	}
	if inSingle || inDouble {
		return nil, errors.New("unterminated quote")
	}
	flush()
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	if len(args) > MaxArgs {
		return nil, fmt.Errorf("command has %d arguments, limit is %d", len(args), MaxArgs)
	}
	for _, a := range args {
		if strings.TrimSpace(a) == "" {
			return nil, errors.New("empty argument")
		}
	}
	return args, nil
}

// needsEscape reports whether a backslash before r should be consumed as an
// escape. Windows paths ("C:\work") keep their separators.
func needsEscape(r rune) bool {
	switch r {
	case '\\', '\'', '"':
		return true
	}
	return unicode.IsSpace(r)
}
