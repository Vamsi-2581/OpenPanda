// SPDX-License-Identifier: AGPL-3.0-or-later

package askengine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Xustalis/OpenPanda/internal/contract"
)

// restrictAcceptanceToProject keeps only the acceptance checks the workspace
// actually declares. The entry model selects from the enumerated shortlist,
// but selection is enforced here rather than trusted to the prompt: an
// invented command would eventually run under the orchestrator's own
// authority, and "the model was told not to" is not a security boundary.
//
// Artifact checks survive untouched — they assert paths inside the workspace,
// and the contract validator already refuses anything that escapes it. A
// command must match a candidate exactly; a model that wanted
// "go test ./internal/x" when the project declares "go test ./..." gets no
// contract rather than a check nobody agreed to.
func restrictAcceptanceToProject(specJSON, workDir string) string {
	if strings.TrimSpace(specJSON) == "" || workDir == "" {
		return specJSON
	}
	var spec map[string]json.RawMessage
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return specJSON
	}
	raw, ok := spec["acceptance"]
	if !ok {
		return specJSON
	}
	var checks []contract.Check
	if err := json.Unmarshal(raw, &checks); err != nil {
		return specJSON
	}
	allowed := make(map[string]bool)
	for _, c := range contract.Enumerate(workDir) {
		allowed[strings.TrimSpace(c.Run)] = true
	}
	kept := make([]contract.Check, 0, len(checks))
	for _, c := range checks {
		if c.EffectiveKind() == contract.KindArtifact || allowed[strings.TrimSpace(c.Run)] {
			kept = append(kept, c)
		}
	}
	if len(kept) == 0 {
		delete(spec, "acceptance")
	} else {
		blob, err := json.Marshal(kept)
		if err != nil {
			return specJSON
		}
		spec["acceptance"] = blob
	}
	out, err := json.Marshal(spec)
	if err != nil {
		return specJSON
	}
	return string(out)
}

// appendUserAcceptance adds checks the user supplied directly to a task's
// spec. They bypass the project shortlist on purpose: the shortlist exists to
// stop a MODEL from inventing commands, not to stop the person who owns the
// tree from saying how the work should be judged.
func appendUserAcceptance(specJSON string, checks []contract.Check) string {
	if len(checks) == 0 {
		return specJSON
	}
	spec := map[string]json.RawMessage{}
	if strings.TrimSpace(specJSON) != "" && json.Unmarshal([]byte(specJSON), &spec) != nil {
		return specJSON
	}
	var existing []contract.Check
	if raw, ok := spec["acceptance"]; ok {
		_ = json.Unmarshal(raw, &existing)
	}
	seenKey := make(map[string]bool, len(existing))
	seenID := make(map[string]bool, len(existing))
	for _, c := range existing {
		seenKey[checkKey(c)] = true
		seenID[c.ID] = true
	}
	for _, c := range checks {
		key := checkKey(c)
		if seenKey[key] {
			continue
		}
		id := c.ID
		for n := 1; id == "" || seenID[id]; n++ {
			id = fmt.Sprintf("user-%d", n)
		}
		c.ID = id
		seenKey[key] = true
		seenID[id] = true
		existing = append(existing, c)
	}
	blob, err := json.Marshal(existing)
	if err != nil {
		return specJSON
	}
	spec["acceptance"] = blob
	out, err := json.Marshal(spec)
	if err != nil {
		return specJSON
	}
	return string(out)
}

// checkKey identifies a check by what it asserts, so the same command supplied
// twice (once by the model, once by the user) runs once.
func checkKey(c contract.Check) string {
	return c.EffectiveKind() + "|" + strings.TrimSpace(c.Run) + "|" + strings.Join(c.Exists, ",")
}
