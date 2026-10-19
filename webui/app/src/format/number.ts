// SPDX-License-Identifier: AGPL-3.0-or-later

/** Number formatting for the usage cards.
 *
 *  Two shapes, because the cards show two kinds of number: counts people can
 *  read exactly (messages, sessions — grouped: "32,756") and magnitudes that
 *  are only interesting at a glance (tokens — compact: "113.9k"). Separate
 *  from the view so the rounding is testable without a DOM. */

/** Grouped integer for exact counts. `Intl` so the separator follows the
 *  locale (1,234 / 1 234 / 1.234). */
export function groupNumber(n: number): string {
  return Math.round(n).toLocaleString()
}

/** Compact magnitude: 999 → "999", 113916 → "113.9k", 2100000000 → "2.1B".
 *  One decimal, trailing ".0" dropped, so a round number does not read as
 *  more precise than it is. */
export function compactNumber(n: number): string {
  const abs = Math.abs(n)
  const sign = n < 0 ? '-' : ''
  const scale = (v: number, suffix: string) => {
    const fixed = v.toFixed(1)
    return sign + (fixed.endsWith('.0') ? fixed.slice(0, -2) : fixed) + suffix
  }
  if (abs >= 1e9) return scale(abs / 1e9, 'B')
  if (abs >= 1e6) return scale(abs / 1e6, 'M')
  if (abs >= 1e3) return scale(abs / 1e3, 'k')
  return sign + String(Math.round(abs))
}
