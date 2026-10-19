// SPDX-License-Identifier: AGPL-3.0-or-later

// Calendar geometry tests. The failure mode these guard against is not a
// crash — it is a grid that looks fine and is wrong: starting on Monday,
// losing a column, or putting today in the future.

import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import {
  HEATMAP_DAYS,
  HEATMAP_WEEKS,
  dayKey,
  heatGrid,
  heatLevel,
  heatShade,
} from './heatmap.ts'

/** Local midnight for a calendar date — no UTC parsing, no timezone drift. */
function day(y: number, m: number, d: number): Date {
  return new Date(y, m - 1, d)
}

describe('heatGrid', () => {
  it('is 26 columns of 7 days', () => {
    const grid = heatGrid(day(2026, 10, 18))
    assert.equal(grid.length, HEATMAP_WEEKS)
    for (const week of grid) assert.equal(week.length, 7)
    assert.equal(grid.flat().length, HEATMAP_DAYS)
  })

  it('starts on a Sunday and ends on a Saturday', () => {
    const grid = heatGrid(day(2026, 10, 18))
    const first = grid[0]![0]!
    const last = grid[HEATMAP_WEEKS - 1]![6]!
    assert.equal(new Date(first.date + 'T00:00:00').getDay(), 0, first.date)
    assert.equal(new Date(last.date + 'T00:00:00').getDay(), 6, last.date)
  })

  it('puts today in the last column and leaves the rest of it blank', () => {
    // 2026-10-18 is a Sunday: the current week has six more days ahead of it,
    // which must be marked future rather than drawn as empty squares.
    const grid = heatGrid(day(2026, 10, 18))
    const lastWeek = grid[HEATMAP_WEEKS - 1]!
    assert.equal(lastWeek[0]!.date, '2026-10-18')
    assert.equal(lastWeek[0]!.future, false)
    for (let i = 1; i < 7; i++) {
      assert.equal(lastWeek[i]!.future, true, `cell ${i} should be future`)
    }
  })

  it('covers the 181 days before the end of this week', () => {
    const grid = heatGrid(day(2026, 10, 18))
    const flat = grid.flat()
    // The window ends on Saturday 2026-10-24, so it opens 181 days earlier —
    // a Sunday, which is what makes every column a whole week.
    assert.equal(flat[0]!.date, '2026-04-26')
    assert.equal(flat[HEATMAP_DAYS - 1]!.date, '2026-10-24')
  })

  it('keeps the window stable as the week advances', () => {
    // Same week, different days in it: identical grid, only `future` moves.
    const sunday = heatGrid(day(2026, 10, 18))
    const thursday = heatGrid(day(2026, 10, 22))
    assert.deepEqual(
      sunday.flat().map((c) => c.date),
      thursday.flat().map((c) => c.date),
    )
    assert.equal(thursday.flat().filter((c) => c.future).length, 2)
  })
})

describe('heatLevel', () => {
  it('buckets proportionally to the busiest day', () => {
    assert.equal(heatLevel(0, 8), 0)
    assert.equal(heatLevel(1, 8), 1)
    assert.equal(heatLevel(2, 8), 1)
    assert.equal(heatLevel(3, 8), 2)
    assert.equal(heatLevel(5, 8), 3)
    assert.equal(heatLevel(7, 8), 4)
    assert.equal(heatLevel(8, 8), 4)
  })

  it('never divides by an empty window', () => {
    assert.equal(heatLevel(3, 0), 0)
    assert.equal(heatLevel(0, 0), 0)
  })

  it('tops out at the four shades the palette defines', () => {
    for (const count of [1, 9, 99, 1000]) {
      assert.ok(heatLevel(count, 10) <= 4)
    }
  })
})

describe('heatShade', () => {
  it('matches the reference ramp: 72 / 64 / 56 / 48 percent lightness', () => {
    assert.equal(heatShade(1), 'hsl(217 70% 72%)')
    assert.equal(heatShade(2), 'hsl(217 70% 64%)')
    assert.equal(heatShade(3), 'hsl(217 70% 56%)')
    assert.equal(heatShade(4), 'hsl(217 70% 48%)')
  })

  it('leaves level 0 to the caller (the neutral surface token)', () => {
    assert.equal(heatShade(0), undefined)
  })
})

describe('dayKey', () => {
  it('formats the local day, not the UTC one', () => {
    assert.equal(dayKey(day(2026, 1, 5)), '2026-01-05')
    // 00:30 local on the 5th is still the 5th here, whatever UTC says.
    const early = new Date(2026, 0, 5, 0, 30)
    assert.equal(dayKey(early), '2026-01-05')
  })
})
