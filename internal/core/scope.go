// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"path"
	"sort"
	"strings"

	"github.com/Xustalis/OpenPanda/internal/scheduler/queue"
)

// maxScopePaths bounds how many scope leaves one delegate request may
// declare — a child holding dozens of locks is a worse-than-monolithic
// claim, so the cap doubles as the "you are not really scoped" boundary.
const maxScopePaths = 8

// normalizeScopePaths cleans declared project-relative paths into canonical
// scope leaves: separators fold to "/", "." and ".." resolve against the
// project root (an escaping claim clamps to the root rather than silently
// leaking outside the project), and any leaf nested inside another declared
// leaf is redundant and dropped. An all-invalid or empty declaration leaves
// nothing — the caller then falls back to the ordinary project key, which
// is the honest serialization for an unscoped claim.
func normalizeScopePaths(paths []string) []string {
	seen := map[string]bool{}
	var clean []string
	for _, p := range paths {
		p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
		if p == "" {
			continue // a blank entry is a malformed list element, not a root claim
		}
		p = path.Clean("/" + p)
		p = strings.TrimPrefix(p, "/")
		if seen[p] {
			continue
		}
		seen[p] = true
		clean = append(clean, p)
	}
	// Shallowest-first so a nested leaf can never shadow its ancestor.
	sort.SliceStable(clean, func(i, j int) bool {
		return strings.Count(clean[i], "/") < strings.Count(clean[j], "/")
	})
	var out []string
	for _, p := range clean {
		nested := false
		for _, kept := range out {
			if kept == p || (kept != "" && strings.HasPrefix(p, kept+"/")) {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, p)
		}
		if len(out) >= maxScopePaths {
			break
		}
	}
	return out
}

// scopeLeafKeys renders the exec/queue lock set for one scoped child.
func scopeLeafKeys(project string, paths []string) []string {
	keys := make([]string, 0, len(paths))
	for _, p := range paths {
		keys = append(keys, queue.ScopeLeafKey(project, p))
	}
	return keys
}

// childResourceKeys computes the lock set a delegate child holds. An
// isolated child works in a private copy, so it locks nothing shared — just
// its own id (plus actuators, which are physical and always exclusive). A
// child that declares project-relative paths gets the matching scope leaves —
// siblings with disjoint scopes then run in parallel inside the same
// worktree, while any task holding the project root (an unscoped claim, a
// fresh whole-project task) still conflicts normally. A child without a
// declaration keeps the monolithic project serialization: an agent that
// cannot name its scope cannot be allowed to narrow its locks on a guess.
func childResourceKeys(taskID string, in TaskInput) []string {
	if in.Project != "" && in.Isolate {
		return mergeActuatorKeys([]string{"task:" + taskID}, in.Requires, in.SpecJSON)
	}
	if in.Project != "" {
		if paths := normalizeScopePaths(in.Paths); len(paths) > 0 {
			return mergeActuatorKeys(scopeLeafKeys(in.Project, paths), in.Requires, in.SpecJSON)
		}
	}
	return deriveResourceKeys(in)
}
