// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !lite

package main

// The roster picker: the TUI's twin of the console's "who works on this
// conversation" card (webui/app/src/components/team-picker.tsx).
//
// Space toggles an agent, Enter applies the working set, Esc discards it. The
// first row is the automatic choice — an empty roster, meaning the scheduler
// picks the best hand. An agent that is not installed here is still listed and
// still selectable: a roster may name hands that only a peer node carries.
//
// Applying writes the bound session's Team (or the bare conversation's roster
// file) exactly like /team does, so the next ask carries the same AskScope the
// web console sends.

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Xustalis/OpenPanda/internal/agents"
	"github.com/Xustalis/OpenPanda/internal/i18n"
)

// teamCheck is the marker a selected row wears. ASCII terminals get a plain
// asterisk so the column still reads on a bare Linux console.
func teamCheck(th theme) string {
	return th.glyph("✓", "*")
}

// agentInstalled reports whether any of a registry entry's binaries resolve on
// PATH. Cheap by design: the picker must open instantly, and the version probe
// the web panel runs is not worth a 5-second-per-agent wait on a keypress.
func agentInstalled(k agents.Known) bool {
	for _, bin := range k.Binaries {
		if _, err := exec.LookPath(bin); err == nil {
			return true
		}
	}
	return false
}

// buildTeamItems draws the automatic row plus every registry agent, marking
// the current working set.
func (m tuiModel) buildTeamItems() []SelectionItem {
	checked := teamCheck(m.th)
	items := make([]SelectionItem, 0, len(agents.Registry())+1)
	autoBadge := ""
	if len(m.teamMarks) == 0 {
		autoBadge = checked
	}
	items = append(items, SelectionItem{
		ID:      "",
		Title:   i18n.T(m.loc, "tui.team.auto"),
		Snippet: i18n.T(m.loc, "tui.team.autoHint"),
		Badge:   autoBadge,
	})
	for _, k := range agents.Registry() {
		name := k.DisplayName
		if name == "" {
			name = k.Name
		}
		snippet := strings.Join(k.DefaultBestAt, " · ")
		if snippet == "" {
			snippet = k.PrimaryBinary()
		}
		if cost := strings.TrimSpace(k.DefaultCostTier); cost != "" {
			snippet += " · " + cost
		}
		if !agentInstalled(k) {
			snippet += " · " + i18n.T(m.loc, "tui.team.notInstalled")
		}
		badge := ""
		if m.teamMarks[k.Name] {
			badge = checked
		}
		items = append(items, SelectionItem{
			ID:      k.Name,
			Title:   name,
			Snippet: snippet,
			Badge:   badge,
		})
	}
	return items
}

// rebuildTeamPicker refreshes labels after a toggle while keeping the cursor
// on the row the user is looking at.
func (m *tuiModel) rebuildTeamPicker() {
	cur := m.selectionList.Cursor
	top := m.selectionList.Top
	m.selectionList.SetItems(m.buildTeamItems())
	m.selectionList.Cursor = cur
	m.selectionList.Top = top
}

// openTeamPicker opens the roster card with the active conversation's current
// pick marked.
func (m tuiModel) openTeamPicker() (tuiModel, tea.Cmd) {
	marks := make(map[string]bool)
	for _, n := range m.r.teamForAsk() {
		marks[n] = true
	}
	m.teamMarks = marks

	sl := NewSelectionList(i18n.T(m.loc, "tui.team.title"), m.buildTeamItems())
	sl.EmptyText = i18n.T(m.loc, "tui.team.empty")
	sl.FooterHints = i18n.T(m.loc, "tui.team.footer")
	// Land on the first selected agent when there is one; the automatic row is
	// a fine default otherwise.
	for i, it := range sl.Items {
		if it.ID != "" && marks[it.ID] {
			sl.Cursor = i
			break
		}
	}
	m.mode = modeList
	m.listKind = listTeam
	m.selectionList = sl
	return m, nil
}

// toggleTeamMark flips the row under the cursor. The automatic row is not a
// checkbox: landing on it means "no roster", so space clears the marks, which
// also makes the row's checkmark appear immediately.
func (m *tuiModel) toggleTeamMark() {
	it, ok := m.selectionList.Selected()
	if !ok {
		return
	}
	if it.ID == "" {
		m.teamMarks = map[string]bool{}
		m.rebuildTeamPicker()
		return
	}
	if m.teamMarks[it.ID] {
		delete(m.teamMarks, it.ID)
	} else {
		m.teamMarks[it.ID] = true
	}
	m.rebuildTeamPicker()
}

// applyTeamPicker commits the working set. Registry order (not click order) is
// the stored order, so the same pick always produces the same roster.
func (m tuiModel) applyTeamPicker() (tea.Model, tea.Cmd) {
	it, ok := m.selectionList.Selected()
	m.mode = modeIdle
	// Enter on the automatic row clears the roster even when other rows are
	// marked: that row is the explicit "no restriction" answer.
	var names []string
	if !ok || it.ID == "" {
		names = nil
	} else {
		for _, k := range agents.Registry() {
			if m.teamMarks[k.Name] {
				names = append(names, k.Name)
			}
		}
	}
	if err := m.r.setTeam(names); err != nil {
		return m, m.printBlock(block{kind: blockError, body: err.Error()})
	}
	m.team = m.r.teamNow()
	m.teamMarks = nil
	body := i18n.T(m.loc, "repl.team.cleared")
	if len(m.team) > 0 {
		body = i18n.Tf(m.loc, "repl.team.set", "names", strings.Join(m.team, ", "))
	}
	return m, m.printBlock(block{kind: blockNote, body: body})
}

// applyTeamCommand is the argument form of /team, kept inline so the status
// badge follows the write immediately.
func (m tuiModel) applyTeamCommand(arg string) (tea.Model, tea.Cmd) {
	msg, err := m.r.teamCommand(arg)
	if err != nil {
		return m, m.printBlock(block{kind: blockError, body: err.Error()})
	}
	m.team = m.r.teamNow()
	return m, m.printBlock(block{kind: blockNote, body: msg})
}
