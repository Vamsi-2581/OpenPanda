// SPDX-License-Identifier: AGPL-3.0-or-later

package entry

import (
	"strings"
	"testing"

	"github.com/Xustalis/OpenPanda/internal/contract"
)

// TestValidateTaskSpecAcceptance: a model-emitted contract is normalized and
// checked at the boundary. IDs are assigned so the executor and every
// renderer agree on the check's name, and shell syntax is refused because the
// runner executes argv directly.
func TestValidateTaskSpecAcceptance(t *testing.T) {
	spec := &TaskSpec{
		Title:       "fix parser",
		ContextType: "command",
		Requires:    Requires{Abilities: []string{"coding"}},
		Spec: TaskSpecDetail{
			Acceptance: []contract.Check{
				{Run: "make test"},
				{Exists: []string{"dist/app"}},
			},
		},
	}
	if err := ValidateTaskSpec(spec); err != nil {
		t.Fatalf("valid contract rejected: %v", err)
	}
	if spec.Spec.Acceptance[0].ID != "check-1" || spec.Spec.Acceptance[1].ID != "check-2" {
		t.Fatalf("ids not assigned: %+v", spec.Spec.Acceptance)
	}
	if spec.Spec.Acceptance[1].EffectiveKind() != contract.KindArtifact {
		t.Fatalf("artifact kind = %q", spec.Spec.Acceptance[1].EffectiveKind())
	}

	bad := &TaskSpec{
		Title:       "fix parser",
		ContextType: "command",
		Requires:    Requires{Abilities: []string{"coding"}},
		Spec: TaskSpecDetail{
			Acceptance: []contract.Check{{Run: "make test && rm -rf /"}},
		},
	}
	if err := ValidateTaskSpec(bad); err == nil || !strings.Contains(err.Error(), "shell syntax") {
		t.Fatalf("shell contract accepted: %v", err)
	}
}
