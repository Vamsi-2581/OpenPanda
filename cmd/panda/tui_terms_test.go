// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !lite

package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Xustalis/OpenPanda/internal/config"
	"github.com/Xustalis/OpenPanda/internal/i18n"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// allLocales is every locale the TUI can be configured with, so a parity gap in
// the terms card cannot hide behind the one the tests happen to run.
var allLocales = []i18n.Locale{
	i18n.English, i18n.ChineseSimp, i18n.Japanese, i18n.Spanish, i18n.German,
}

// termsModel builds a model parked on the terms step at the given size.
func termsModel(t *testing.T, loc i18n.Locale, w, h int, reconsent bool) tuiModel {
	t.Helper()
	r := &repl{loc: loc, cfg: &config.Config{}}
	m := newTUIModel(r)
	m.mode = modeOnboarding
	m.onboardingStep = onboardingStepTerms
	m.termsReconsent = reconsent
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(tuiModel)
}

// TestTermsCardHasEveryClauseInEveryLocale guards the parity the web console
// also relies on: the ten clauses are the same notice on both surfaces, and a
// locale that silently lost one would ship a weaker disclaimer than the others.
func TestTermsCardHasEveryClauseInEveryLocale(t *testing.T) {
	for _, loc := range allLocales {
		tt := termsCardText(loc)
		if tt.heading == "" || tt.prevails == "" || tt.agree == "" || tt.decline == "" || tt.hints == "" {
			t.Fatalf("%s: terms card has an empty fixed string: %+v", loc, tt)
		}
		if len(tt.sections) != 10 {
			t.Fatalf("%s: got %d clauses, want 10", loc, len(tt.sections))
		}
		for i, sec := range tt.sections {
			if strings.TrimSpace(sec.title) == "" || strings.TrimSpace(sec.body) == "" {
				t.Fatalf("%s: clause %d is empty: %+v", loc, i+1, sec)
			}
			// The clause number leads the title, so the card reads as the
			// numbered document the web shows.
			if !strings.HasPrefix(sec.title, fmt.Sprintf("%d.", i+1)) {
				t.Fatalf("%s: clause %d title %q does not start with %q", loc, i+1, sec.title, fmt.Sprintf("%d.", i+1))
			}
		}
	}
}

// TestTermsCardFitsTheTerminal is the regression guard for the ten-clause card:
// the four-clause summary it replaced already overran an 80x24 terminal, and the
// full document is twice its length. Whatever the size, the card must stay
// inside the viewport, must not push a row past the frame's inner width, must
// open on clause one, must reach the end of the document, and must keep the
// answer row on screen — the answer is the only thing the user has to act on.
func TestTermsCardFitsTheTerminal(t *testing.T) {
	sizes := [][2]int{{120, 50}, {100, 30}, {80, 24}, {60, 24}, {50, 20}}
	for _, loc := range allLocales {
		for _, size := range sizes {
			for _, reconsent := range []bool{false, true} {
				w, h := size[0], size[1]
				m := termsModel(t, loc, w, h, reconsent)

				v := m.View()
				lines := strings.Split(v, "\n")
				if len(lines) > h {
					t.Fatalf("%s %dx%d reconsent=%v: card is %d rows, terminal has %d:\n%s",
						loc, w, h, reconsent, len(lines), h, v)
				}
				for i, line := range lines {
					if got := lipgloss.Width(line); got > w {
						t.Fatalf("%s %dx%d reconsent=%v: row %d is %d columns wide:\n%s",
							loc, w, h, reconsent, i, got, v)
					}
				}

				tt := termsCardText(loc)
				inner := termsBoxWidth(w) - 2
				_, body, _ := m.termsCard(tt, termsBoxWidth(w), "")
				for i, line := range body {
					if got := lipgloss.Width(line); got > inner {
						t.Fatalf("%s %dx%d reconsent=%v: clause row %d is %d wide, frame holds %d: %q",
							loc, w, h, reconsent, i, got, inner, line)
					}
				}

				if plain := ansi.Strip(v); !strings.Contains(plain, tt.sections[0].title) {
					t.Fatalf("%s %dx%d reconsent=%v: card does not open on clause one:\n%s",
						loc, w, h, reconsent, v)
				}

				// Scrolling to the end must land on the end of the document.
				m.termsScroll = 1 << 20
				m.scrollTerms(0)
				end := ansi.Strip(m.View())
				if !strings.Contains(end, tailOf(body)) {
					t.Fatalf("%s %dx%d reconsent=%v: end of the document is unreachable:\n%s",
						loc, w, h, reconsent, end)
				}
				if !strings.Contains(end, tt.agree) || !strings.Contains(end, tt.decline) {
					t.Fatalf("%s %dx%d reconsent=%v: answer row scrolled away with the body:\n%s",
						loc, w, h, reconsent, end)
				}
			}
		}
	}
}

// tailOf is the last non-blank clause row, so "reached the end" can be asserted
// without depending on the final clause's title still being inside the window.
func tailOf(body []string) string {
	for i := len(body) - 1; i >= 0; i-- {
		s := strings.TrimSpace(ansi.Strip(body[i]))
		if s == "" {
			continue
		}
		if len(s) > 12 {
			return s[len(s)-12:]
		}
		return s
	}
	return ""
}

// TestTermsCardScrollClamps covers the window's edges: it never scrolls past the
// document, never before it, and a resize cannot leave the offset stranded.
func TestTermsCardScrollClamps(t *testing.T) {
	m := termsModel(t, i18n.English, 100, 30, false)

	m.scrollTerms(-5)
	if m.termsScroll != 0 {
		t.Fatalf("scrolling up from the top moved to %d, want 0", m.termsScroll)
	}

	m.scrollTerms(1 << 20)
	head, body, foot := m.termsCard(termsCardText(m.loc), termsBoxWidth(m.width), "")
	rows := m.termsBodyRows(len(head), len(foot))
	if want := len(body) - rows; m.termsScroll != want {
		t.Fatalf("scrolling to the end stopped at %d, want %d", m.termsScroll, want)
	}

	// Shrinking the terminal must not strand the offset past the new end: the
	// view clamps what it renders, and the next scroll settles the state too.
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	m = next.(tuiModel)
	m.scrollTerms(0)
	head, body, foot = m.termsCard(termsCardText(m.loc), termsBoxWidth(m.width), "")
	rows = m.termsBodyRows(len(head), len(foot))
	if m.termsScroll > len(body)-rows {
		t.Fatalf("after resize the offset %d exceeds the end %d", m.termsScroll, len(body)-rows)
	}
}

// TestWrapTermsText pins the wrapper's contract: it fills the width, never
// opens a line with the space that ended the previous one, and never emits an
// empty line. lipgloss's own wrapper fails the first of those on CJK, which is
// why the card carries its own.
func TestWrapTermsText(t *testing.T) {
	const width = 40
	cases := map[string]string{
		"latin prose":  "OpenPanda can run shell commands, modify files and alter system state, including on remote nodes.",
		"cjk prose":    "你的源代码、工作区文件、对话记录、记忆库与 API 凭据默认保留在你自己的设备上。软件不含遥测、跟踪或私人数据收集。",
		"mixed":        "OpenPanda 是基于 GNU Affero 通用公共许可证 3.0 或更高版本（AGPL-3.0-or-later）发布的自由开源软件。",
		"one long run": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"spaced out":   "a b c d e f g h i j k l m n o p q r s t u v w x y z",
	}
	for name, in := range cases {
		lines := wrapTermsText(in, width)
		if len(lines) == 0 {
			t.Fatalf("%s: produced no lines", name)
		}
		for i, line := range lines {
			if strings.TrimSpace(line) == "" {
				t.Fatalf("%s: line %d is blank", name, i)
			}
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("%s: line %d is %d columns, want <= %d: %q", name, i, got, width, line)
			}
			if line != strings.TrimLeft(line, " ") {
				t.Fatalf("%s: line %d opens with a space: %q", name, i, line)
			}
			if line != strings.TrimRight(line, " ") {
				t.Fatalf("%s: line %d ends with a space: %q", name, i, line)
			}
		}
		// No text may be dropped on the floor. Whitespace is stripped rather
		// than normalised: a break point legitimately swallows the space it
		// broke on, and CJK runs break with no space at all, so re-joining the
		// lines cannot reproduce the original spacing. Every non-space
		// character must survive in order.
		got := stripSpace(strings.Join(lines, ""))
		want := stripSpace(in)
		if got != want {
			t.Fatalf("%s: wrapping changed the text\n got: %q\nwant: %q", name, got, want)
		}
	}
}

// stripSpace drops every whitespace rune, so two wrappings of the same text can
// be compared regardless of where the breaks landed.
func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' {
			return -1
		}
		return r
	}, s)
}

// TestWrapTermsTextHonoursLineBreakRules pins the two prohibitions that make the
// Chinese card read as typeset rather than as a hard-wrapped dump: a line may not
// open on a closing bracket, and a Latin token that fits on a line is not split
// across two. Both are visible in the shipped Chinese clauses — "（AGPL-3.0-or-
// later）" and "（包括商业用途" / "），但" — so a regression is not hypothetical.
func TestWrapTermsTextHonoursLineBreakRules(t *testing.T) {
	const width = 40
	texts := []string{
		"OpenPanda 是基于 GNU Affero 通用公共许可证 3.0 或更高版本（AGPL-3.0-or-later）发布的自由开源软件。",
		"你可以运行、研究、修改和再分发（包括商业用途），但衍生作品必须同样以 AGPL 发布。",
		"完整许可与条款见随附的 LICENSE 与 TERMS.zh-CN.md 文件，如与本摘要不一致，以二者为准。",
	}
	for _, in := range texts {
		lines := wrapTermsText(in, width)
		for i, line := range lines {
			runes := []rune(line)
			if last := runes[len(runes)-1]; strings.ContainsRune(noLineEnd, last) {
				t.Fatalf("line %d ends on an opening bracket %q: %q", i, string(last), line)
			}
			if first := runes[0]; strings.ContainsRune(noLineStart, first) {
				t.Fatalf("line %d opens on closing punctuation %q: %q", i, string(first), line)
			}
		}
		// A Latin token that fits on a line must survive whole.
		for _, token := range []string{"AGPL-3.0-or-later", "COMMERCIAL.md", "TERMS.zh-CN.md", "OpenPanda"} {
			if !strings.Contains(in, token) {
				continue
			}
			if !strings.Contains(strings.Join(lines, "\n"), token) {
				t.Fatalf("token %q was split across lines in %q:\n%s", token, in, strings.Join(lines, "\n"))
			}
		}
	}
}
