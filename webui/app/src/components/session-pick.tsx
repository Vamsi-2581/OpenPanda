// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import type { Session } from '../api/client'
import { t } from '../i18n'
import { Icon } from './icons'
import { sortSessions } from './session-list'
import { fmtElapsed } from './task-activity'

/** The composer footer's thread pick: which conversation the next task is
 *  appended to, chosen without ever opening it.
 *
 *  工作空间 never paints a transcript — that is the point of the surface — so
 *  "continue an existing thread" has to be a pick, not a navigation. The pill
 *  sits beside the directory pill in the same footer and speaks the same
 *  grammar (icon, one line, ellipsis); the card above it lists the threads
 *  that directory already owns, newest first, with the last thing each one
 *  said as its second line. That preview is what makes a title like
 *  "fix the login bug" distinguishable from the four other threads that
 *  started the same way.
 *
 *  Nothing here writes: a pick only changes what the composer's send targets,
 *  and the × / 新对话 row puts it back to "mint a fresh thread". */
export function SessionPick(props: {
  /** Threads of the directory currently picked (already scoped by project). */
  sessions: Session[]
  value: Session | null
  onChange(session: Session | null): void
  /** A picker with no directory has nothing to scope to — it says so instead
   *  of listing every thread in the store. */
  hasDir: boolean
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const box = useRef<HTMLDivElement>(null)

  // Newest activity first, pinned threads leading — the same order the
  // sidebar's history uses, so a thread sits where the reader last saw it.
  const ordered = useMemo(() => sortSessions(props.sessions), [props.sessions])
  const rows = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return ordered
    return ordered.filter((s) => {
      const last = s.turns?.[s.turns.length - 1]?.text ?? ''
      return (s.title || '').toLowerCase().includes(q) || last.toLowerCase().includes(q)
    })
  }, [ordered, query])

  // A new picker mount (or a directory switch) starts closed and unsearched.
  useEffect(() => {
    setOpen(false)
    setQuery('')
  }, [props.hasDir])

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.stopPropagation()
      setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey, true)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey, true)
    }
  }, [open])

  const label = props.value ? props.value.title || t('sessions.untitled') : t('workspace.pickSession')

  return (
    <div class="sp-picker" ref={box}>
      <button
        type="button"
        class={`workdir-btn sp-btn${props.value ? ' has-value' : ''}`}
        /* The bubble is the full title: the pill is one line and a thread name
           is routinely longer than the footer can pay for. Only while closed —
           the card above already says everything the tip would. */
        data-tip={!open && props.value ? props.value.title : undefined}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={t('workspace.pickSession')}
        disabled={props.disabled}
        onClick={() => setOpen((v) => !v)}
      >
        <Icon name="message" size={13} />
        <span class="workdir-path">
          {/* The lead word is what makes an armed pill unambiguous in either
              theme: the fill tint alone is one shade of grey in dark, and
              "appending to a conversation" is exactly the state a reader must
              not have to squint at. */}
          {props.value && (
            <span class="sp-lead">{t('workspace.pickSessionContinue')}</span>
          )}
          {label}
        </span>
        <Icon name="chevron-down" size={12} class="sp-caret" />
      </button>
      {open && (
        <div class="sp-panel" role="dialog" aria-label={t('workspace.pickSession')}>
          <input
            class="input sp-search"
            type="search"
            value={query}
            placeholder={t('workspace.pickSessionSearch')}
            aria-label={t('workspace.pickSessionSearch')}
            autoFocus
            onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
          />
          <div class="sp-list" role="listbox" aria-label={t('workspace.pickSession')}>
            {/* The way back to a brand-new thread: one row, always first, so
                "continue something" is a pick and "start over" is a pick too. */}
            <button
              type="button"
              class={`sp-row sp-row-new${props.value ? '' : ' sel'}`}
              role="option"
              aria-selected={!props.value}
              onClick={() => {
                props.onChange(null)
                setOpen(false)
              }}
            >
              <Icon name="square-pen" size={14} class="sp-row-icon" />
              <span class="sp-row-text">
                <span class="sp-row-title">{t('workspace.pickSessionNew')}</span>
              </span>
            </button>
            {!props.hasDir ? (
              <p class="sp-empty">{t('workspace.pickSessionNoDir')}</p>
            ) : rows.length === 0 ? (
              <p class="sp-empty">
                {props.sessions.length === 0
                  ? t('workspace.pickSessionEmpty')
                  : t('workspace.pickSessionNoMatch')}
              </p>
            ) : (
              rows.map((s) => {
                const last = s.turns?.[s.turns.length - 1]
                const running = s.operation?.status === 'running'
                return (
                  <button
                    type="button"
                    key={s.id}
                    class={`sp-row${props.value?.id === s.id ? ' sel' : ''}`}
                    role="option"
                    aria-selected={props.value?.id === s.id}
                    title={s.title || t('sessions.untitled')}
                    onClick={() => {
                      props.onChange(s)
                      setOpen(false)
                    }}
                  >
                    <span class="sp-row-text">
                      <span class="sp-row-title">
                        {running && (
                          <span class="sp-run" title={t('orch.doing')} aria-label={t('orch.doing')} />
                        )}
                        {s.title || t('sessions.untitled')}
                      </span>
                      {last?.text && <span class="sp-row-preview">{last.text}</span>}
                    </span>
                    <span class="sp-row-age">{fmtElapsed(s.updated_at)}</span>
                  </button>
                )
              })
            )}
          </div>
        </div>
      )}
    </div>
  )
}
