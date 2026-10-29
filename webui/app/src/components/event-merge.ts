// SPDX-License-Identifier: AGPL-3.0-or-later

import type { TaskEvent } from '../api/client'

/** Append the rows an incremental read returned, dropping any already held.
 *
 *  The wire carries the task_events id now, so this is an id-keyed merge; a
 *  row without one (an older panel) falls back to its own content as identity.
 *  The result stays in id order, which is the order the panel serves a
 *  timeline in and the order the timeline renders.
 *
 *  This is what lets a page that is already showing a long run poll with a
 *  cursor: the response carries only new rows, and the transcript it renders
 *  stays whole. */
export function mergeEvents(
  prev: TaskEvent[] | undefined,
  next: TaskEvent[] | undefined,
): TaskEvent[] {
  const held = prev ?? []
  const fresh = next ?? []
  if (fresh.length === 0) return held
  if (held.length === 0) return [...fresh].sort(byCursor)

  const seen = new Set<string>()
  const out: TaskEvent[] = []
  for (const e of held) {
    seen.add(identity(e))
    out.push(e)
  }
  for (const e of fresh) {
    const k = identity(e)
    if (seen.has(k)) continue
    seen.add(k)
    out.push(e)
  }
  out.sort(byCursor)
  return out
}

/** The newest cursor in a batch — what the next read asks to resume from. */
export function newestCursor(events: TaskEvent[] | undefined, floor = 0): number {
  let max = floor
  for (const e of events ?? []) {
    if (typeof e.id === 'number' && e.id > max) max = e.id
  }
  return max
}

function identity(e: TaskEvent): string {
  return typeof e.id === 'number' ? `id:${e.id}` : `row:${e.ts}|${e.type}|${e.data}`
}

function byCursor(a: TaskEvent, b: TaskEvent): number {
  const ai = typeof a.id === 'number' ? a.id : 0
  const bi = typeof b.id === 'number' ? b.id : 0
  if (ai !== bi) return ai - bi
  return (a.ts ?? 0) - (b.ts ?? 0)
}
