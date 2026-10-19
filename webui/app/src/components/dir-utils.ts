// SPDX-License-Identifier: AGPL-3.0-or-later

/** Extract a clean project name candidate from an absolute directory path */
export function suggestProjectName(dirPath: string): string {
  if (!dirPath) return ''
  const trimmed = dirPath.replace(/[/\\]+$/, '')
  const parts = trimmed.split(/[/\\]/)
  const last = parts[parts.length - 1] || ''
  return last.replace(/[^\w\u4e00-\u9fa5-]/g, '-').replace(/^-+|-+$/g, '') || 'project'
}

/** The folder's own name — the last segment of a path, which is what a
 *  readout shows when the full path is one hover away. Unlike
 *  `suggestProjectName` this is a *label*, not an identifier: it keeps the
 *  name verbatim (no character scrubbing) and answers with the input itself
 *  when there is no segment to take (a bare root, or an empty value). */
export function lastPathSegment(path: string): string {
  if (!path) return ''
  const trimmed = path.replace(/[/\\]+$/, '')
  // "/" and "C:\" trim to "" and "C:" — the first has no name at all, the
  // second is the drive, which is the closest thing to one.
  if (!trimmed) return path
  const parts = trimmed.split(/[/\\]/)
  return parts[parts.length - 1] || path
}
