// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useRef, useState } from 'preact/hooks'
import { t } from '../i18n'
import { Icon } from './icons'

/** The five permission policies a conversation can run under.
 *
 *  They answer a coarser question than the approval switch beside them: the
 *  switch grants *this turn* the right to run irreversible operations, while
 *  the mode decides what the thread may do at all, turn after turn. The two
 *  do not overlap — bypass consents to everything, manual and plan consent to
 *  nothing — which is why the console hides the switch in those modes instead
 *  of showing a control that cannot change the outcome. */
export type PermissionMode = 'auto' | 'manual' | 'edits' | 'plan' | 'bypass'

export const PERMISSION_MODES: PermissionMode[] = ['auto', 'manual', 'edits', 'plan', 'bypass']

/** Modes where an explicit per-turn consent still means something. */
export function authorizeRelevant(mode: PermissionMode): boolean {
  return mode === 'auto' || mode === 'edits'
}

/** What an unset mode reads as. The server normalizes the same way. */
export function normalizePermission(raw: string | undefined | null): PermissionMode {
  switch (raw) {
    case 'manual':
    case 'edits':
    case 'plan':
    case 'bypass':
      return raw
    default:
      return 'auto'
  }
}

/** The mode chip and its menu.
 *
 *  The trigger lives in the composer's action row, where the old authorize
 *  switch sat: permission is a property of sending, and a control that
 *  changes what Enter means belongs next to the box it sends from. The menu
 *  opens upward — the composer sits at the bottom of the pane — and every row
 *  carries its own one-line consequence, because "接受编辑" without "shell 命令
 *  仍会询问" is a promise the gate does not keep. */
export function PermissionPicker(props: {
  value: PermissionMode
  onChange(mode: PermissionMode): void
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey, true)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey, true)
    }
  }, [open])

  const mode = props.value
  return (
    <div class="perm-pick" ref={root}>
      <button
        type="button"
        class={`perm-trigger perm-${mode}${open ? ' open' : ''}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={t('perm.title')}
        title={t(`perm.${mode}Hint`)}
        disabled={props.disabled}
        onClick={() => setOpen((v) => !v)}
      >
        <Icon name={mode === 'bypass' ? 'alert' : 'shield'} size={13} />
        <span>{t(`perm.${mode}`)}</span>
        <Icon name="chevron-down" size={12} />
      </button>
      {open && (
        <div class="perm-menu" role="listbox" aria-label={t('perm.title')}>
          <p class="perm-menu-head">{t('perm.title')}</p>
          {PERMISSION_MODES.map((m) => (
            <button
              key={m}
              type="button"
              role="option"
              aria-selected={m === mode}
              class={`perm-row${m === mode ? ' on' : ''}${m === 'bypass' ? ' danger' : ''}`}
              onClick={() => {
                setOpen(false)
                if (m !== mode) props.onChange(m)
              }}
            >
              <span class="perm-row-icon" aria-hidden="true">
                <Icon name={m === 'bypass' ? 'alert' : 'shield'} size={14} />
              </span>
              <span class="perm-row-text">
                <span class="perm-row-name">{t(`perm.${m}`)}</span>
                <span class="perm-row-hint">{t(`perm.${m}Hint`)}</span>
              </span>
              {m === mode && <Icon name="check" size={14} class="perm-check" />}
            </button>
          ))}
          <p class="perm-foot">{t('perm.foot')}</p>
        </div>
      )}
    </div>
  )
}
