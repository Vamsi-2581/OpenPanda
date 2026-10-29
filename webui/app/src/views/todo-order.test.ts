// SPDX-License-Identifier: AGPL-3.0-or-later

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { reorderIds } from './todo-order.ts'

test('dropping a card on its own slot keeps it where it was', () => {
  const lane = ['a', 'b', 'c']
  // The pointer is over "b" while "b" itself is being dragged.
  assert.deepEqual(reorderIds(lane, lane, 1, 'b'), ['a', 'b', 'c'])
})

test('dropping the last card onto its own slot keeps it last', () => {
  const lane = ['a', 'b', 'c']
  assert.deepEqual(reorderIds(lane, lane, 2, 'c'), ['a', 'b', 'c'])
})

test('dropping before a card inserts ahead of it', () => {
  const lane = ['a', 'b', 'c']
  assert.deepEqual(reorderIds(lane, lane, 0, 'c'), ['c', 'a', 'b'])
})

test('dropping past the last card appends', () => {
  const lane = ['a', 'b', 'c']
  assert.deepEqual(reorderIds(lane, lane, 3, 'a'), ['b', 'c', 'a'])
})

test('a filtered view anchors on the visible neighbour, not its index', () => {
  // The lane holds a,b,c,d but the filter shows a and d. Dropping c before d
  // must land between b and d in the stored order — the hidden half keeps its
  // relative order and is not renumbered from the visible list.
  const lane = ['a', 'b', 'c', 'd']
  const visible = ['a', 'd']
  assert.deepEqual(reorderIds(lane, visible, 1, 'c'), ['a', 'b', 'c', 'd'])
  assert.deepEqual(reorderIds(lane, visible, 0, 'd'), ['d', 'a', 'b', 'c'])
})
