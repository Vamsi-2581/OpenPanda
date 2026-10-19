// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { layerAboveOwnsEscape } from './card-dismiss.ts'

describe('header card dismissal', () => {
  it('lets a menu or modal own Escape before the header card', () => {
    const original = globalThis.document
    try {
      for (const open of [true, false]) {
        globalThis.document = {
          querySelector: () => (open ? {} : null),
        } as unknown as Document
        assert.equal(layerAboveOwnsEscape(), open)
      }
    } finally {
      globalThis.document = original
    }
  })
})
