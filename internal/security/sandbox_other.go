// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !darwin && !linux

package security

import "os/exec"

// platformBackend reports "" — no OS confinement on this platform (Windows's
// Job Object / AppContainer surface is not wired). Callers surface the empty
// backend rather than imply isolation exists, and a strict policy is refused
// instead of run unconfined.
func platformBackend() string { return "" }

func wrapSubprocess(cmd *exec.Cmd, p Policy) error {
	return backendMissing(p.Mode, "no sandbox backend exists on this platform")
}
