// SPDX-License-Identifier: AGPL-3.0-or-later

/** Where a dragged card lands inside its lane's full membership.
 *
 *  The board shows a filtered view but persists a whole lane: dropping while
 *  "已逾期" is selected must not renumber the hidden half. The pointer names a
 *  visible neighbour (or the end of the list); this maps that to an index in
 *  the full lane.
 *
 *  Dropping a card onto its own position is a no-op. The visible anchor would
 *  be the dragged card itself, which is absent from the list being spliced
 *  into, so the lookup would miss and append — the card would jump to the end
 *  of its own lane. Aim at the next visible card instead.
 */
export function reorderIds<T>(
  laneIds: T[],
  visibleIds: T[],
  anchorIndex: number,
  id: T,
): T[] {
  const ids = laneIds.filter((x) => x !== id)
  let anchor = anchorIndex < visibleIds.length ? visibleIds[anchorIndex] : null
  if (anchor === id) anchor = visibleIds[anchorIndex + 1] ?? null
  const at = anchor ? ids.indexOf(anchor) : -1
  ids.splice(at < 0 ? ids.length : at, 0, id)
  return ids
}
