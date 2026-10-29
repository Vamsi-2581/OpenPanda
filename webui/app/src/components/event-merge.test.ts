// SPDX-License-Identifier: AGPL-3.0-or-later

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mergeEvents, newestCursor } from './event-merge.ts'
import type { TaskEvent } from '../api/client'

const ev = (id: number, type = 'progress'): TaskEvent => ({
  id,
  ts: 1_800_000_000 + id,
  type,
  data: '{}',
})

test('an incremental batch appends in id order', () => {
  const merged = mergeEvents([ev(1), ev(2)], [ev(3), ev(4)])
  assert.deepEqual(
    merged.map((e) => e.id),
    [1, 2, 3, 4],
  )
})

test('re-sent rows are dropped rather than duplicated', () => {
  const merged = mergeEvents([ev(1), ev(2), ev(3)], [ev(3), ev(4)])
  assert.deepEqual(
    merged.map((e) => e.id),
    [1, 2, 3, 4],
  )
})

test('a full re-read replaces nothing it does not overlap', () => {
  const merged = mergeEvents(undefined, [ev(2), ev(1)])
  assert.deepEqual(
    merged.map((e) => e.id),
    [1, 2],
  )
})

test('an empty batch leaves the timeline untouched', () => {
  const held = [ev(1), ev(2)]
  assert.equal(mergeEvents(held, []), held)
  assert.deepEqual(mergeEvents(held, undefined), held)
})

test('rows without an id fall back to their content as identity', () => {
  const a: TaskEvent = { ts: 10, type: 'progress', data: '{"note":"x"}' }
  const b: TaskEvent = { ts: 10, type: 'progress', data: '{"note":"x"}' }
  const merged = mergeEvents([a], [b])
  assert.equal(merged.length, 1, 'the same row delivered twice must not double')
})

test('newestCursor is the highest id, never below the floor', () => {
  assert.equal(newestCursor([ev(3), ev(9), ev(5)]), 9)
  assert.equal(newestCursor([]), 0)
  assert.equal(newestCursor(undefined, 7), 7)
  assert.equal(newestCursor([ev(2)], 7), 7, 'the cursor must not go backwards')
})
