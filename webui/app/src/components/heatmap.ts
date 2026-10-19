// SPDX-License-Identifier: AGPL-3.0-or-later

/** The activity calendar's geometry and shading.
 *
 *  Kept out of the view for the same reason format/json.ts is: the test
 *  runner strips types but does not compile JSX, and this is the part that
 *  can be silently wrong (a grid that starts on the wrong weekday, a level
 *  ramp that saturates on day one) without looking broken.
 *
 *  The shape is Claude Desktop's usage calendar, read off its shipped
 *  renderer rather than eyeballed:
 *
 *  - 182 days = 26 whole weeks, so the grid is always exactly 26 columns;
 *  - the window ENDS on the Saturday of the current week, which makes the
 *    leftmost column a Sunday — today sits in the last column, and the days
 *    after it render as nothing at all rather than as empty cells;
 *  - a day's shade is `hsl(217 70% L)` with
 *    `L = 80 - ceil(4 * count / max) * 8`, i.e. 72 / 64 / 56 / 48 for the
 *    four non-empty levels. Proportional to the busiest day, not to fixed
 *    thresholds, so a quiet install still gets a visible ramp.
 */

/** Columns in the calendar — 26 weeks, the reference implementation's window. */
export const HEATMAP_WEEKS = 26

/** Cells in the calendar (26 × 7). */
export const HEATMAP_DAYS = HEATMAP_WEEKS * 7

/** Hue of the shade ramp. Blue in both themes; only lightness moves. */
export const HEATMAP_HUE = 217

/** One square: which day it stands for, and whether that day has happened. */
export interface HeatCell {
  /** Local calendar day, YYYY-MM-DD. */
  date: string
  /** True for days after today: drawn as a gap, never as an empty cell. */
  future: boolean
}

/** A day that actually saw activity, as the API reports it. */
export interface HeatDay {
  date: string
  count: number
}

/** Local calendar day as YYYY-MM-DD — the key every comparison here uses.
 *  Built from local getters, never `toISOString()`, which would shift the
 *  day for anyone east or west of UTC. */
export function dayKey(d: Date): string {
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${d.getFullYear()}-${m}-${day}`
}

/** Midnight local on the same calendar day, so arithmetic below is immune to
 *  whatever time-of-day the caller passed in. */
function startOfDay(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate())
}

/** Adds whole days using local wall-clock arithmetic (setDate), which keeps
 *  the calendar day stable across DST shifts where adding 86_400_000 ms would
 *  skip or repeat an hour. */
function addDays(d: Date, n: number): Date {
  const out = new Date(d)
  out.setDate(out.getDate() + n)
  return out
}

/** The 26×7 grid, column-major: `grid[week][dayOfWeek]`, Sunday first. */
export function heatGrid(today: Date): HeatCell[][] {
  const base = startOfDay(today)
  const todayKey = dayKey(base)
  // Saturday closes the week that holds today; 6 - getDay() is how many days
  // remain until then (0 when today IS Saturday).
  const end = addDays(base, 6 - base.getDay())
  const start = addDays(end, -(HEATMAP_DAYS - 1))

  const weeks: HeatCell[][] = []
  for (let w = 0; w < HEATMAP_WEEKS; w++) {
    const week: HeatCell[] = []
    for (let d = 0; d < 7; d++) {
      const date = dayKey(addDays(start, w * 7 + d))
      week.push({ date, future: date > todayKey })
    }
    weeks.push(week)
  }
  return weeks
}

/** Shade level 0–4 for a day, given the busiest day in the same window.
 *  Identical to the CLI heatmap's `heatLevel` (cmd/panda/heatmap.go) so the
 *  terminal and the console never colour the same day differently. */
export function heatLevel(count: number, max: number): number {
  if (count <= 0 || max <= 0) return 0
  return Math.min(4, Math.ceil((4 * count) / max))
}

/** The CSS colour for a level, or undefined for level 0 (the caller paints
 *  that one with the neutral surface token instead). Lightness is clamped to
 *  a readable band so a future ramp tweak cannot produce white-on-white. */
export function heatShade(level: number): string | undefined {
  if (level <= 0) return undefined
  const lightness = 80 - Math.min(4, level) * 8
  return `hsl(${HEATMAP_HUE} 70% ${lightness}%)`
}
