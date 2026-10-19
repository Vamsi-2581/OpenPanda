// SPDX-License-Identifier: AGPL-3.0-or-later

import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import { compactNumber, groupNumber } from './number.ts'

describe('compactNumber', () => {
  it('leaves counts under a thousand alone', () => {
    assert.equal(compactNumber(0), '0')
    assert.equal(compactNumber(7), '7')
    assert.equal(compactNumber(999), '999')
  })

  it('scales to k / M / B with one decimal', () => {
    assert.equal(compactNumber(1000), '1k')
    assert.equal(compactNumber(113916), '113.9k')
    assert.equal(compactNumber(2_100_000_000), '2.1B')
  })

  it('drops a trailing .0 instead of implying precision', () => {
    assert.equal(compactNumber(2_000_000), '2M')
    assert.equal(compactNumber(12_000), '12k')
  })

  it('keeps the sign', () => {
    assert.equal(compactNumber(-1500), '-1.5k')
  })
})

describe('groupNumber', () => {
  it('groups and rounds to whole numbers', () => {
    // The separator is locale-dependent; the digits are not.
    assert.equal(groupNumber(32756).replace(/\D/g, ''), '32756')
    assert.equal(groupNumber(3.4), '3')
  })
})
