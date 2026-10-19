// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { lastPathSegment, suggestProjectName } from './dir-utils.ts'

describe('suggestProjectName', () => {
  it('handles empty path', () => {
    assert.equal(suggestProjectName(''), '')
  })

  it('extracts folder name from Unix path', () => {
    assert.equal(suggestProjectName('/Users/alice/projects/my-panda'), 'my-panda')
    assert.equal(suggestProjectName('/Users/alice/projects/my-panda/'), 'my-panda')
  })

  it('extracts folder name from Windows path', () => {
    assert.equal(suggestProjectName('C:\\Users\\alice\\projects\\open_panda'), 'open_panda')
    assert.equal(suggestProjectName('C:\\Users\\alice\\projects\\open_panda\\'), 'open_panda')
  })

  it('handles Chinese directory names', () => {
    assert.equal(suggestProjectName('/Users/alice/我的项目'), '我的项目')
  })

  it('sanitizes illegal characters in project names', () => {
    assert.equal(suggestProjectName('/Users/alice/foo@bar#123'), 'foo-bar-123')
  })
})

describe('lastPathSegment', () => {
  it('handles empty path', () => {
    assert.equal(lastPathSegment(''), '')
  })

  it('takes the last segment of a Unix path', () => {
    assert.equal(lastPathSegment('/Users/alice/projects/my-panda'), 'my-panda')
    assert.equal(lastPathSegment('/Users/alice/projects/my-panda/'), 'my-panda')
    assert.equal(lastPathSegment('/Users/alice/projects/my-panda//'), 'my-panda')
  })

  it('takes the last segment of a Windows path', () => {
    assert.equal(lastPathSegment('C:\\Users\\alice\\projects\\open_panda'), 'open_panda')
    assert.equal(lastPathSegment('C:\\Users\\alice\\projects\\open_panda\\'), 'open_panda')
  })

  it('keeps names verbatim, including spaces and CJK', () => {
    assert.equal(lastPathSegment('/Users/alice/我的 项目'), '我的 项目')
    assert.equal(lastPathSegment('D:\\X\\project\\panda'), 'panda')
  })

  it('falls back to the input for a bare root', () => {
    assert.equal(lastPathSegment('/'), '/')
    assert.equal(lastPathSegment('C:\\'), 'C:')
  })
})
