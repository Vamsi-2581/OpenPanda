// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useRef, useState } from 'preact/hooks'
import { api, type ModelEntry } from '../api/client'
import { useAsync } from '../hooks'
import { t } from '../i18n'
import { navigate } from '../nav'
import { Icon } from './icons'
import { toast, toastError } from './toast'

/** The composer's model switch: the model's name and a chevron, sitting
 *  immediately left of Send, opening a card upward.
 *
 *  Read off Codex's own composer rather than invented — the extension's
 *  shipped webview bundle (`openai.chatgpt`, `webview/assets/app-initial-*.js`
 *  and its CSS module `_tovi5_`), where this control is the "intelligence
 *  picker". What that source actually does, and what is copied here:
 *
 *  - The trigger is ghost chrome, not a control: `_ViewToggle_` is 32px tall,
 *    8px radius, 4px padding, and only its *inner* `_ViewToggleContent_`
 *    (2px 4px, 6px radius, 2px gap) takes the hover tint. So the row reads as
 *    the caption of what Send is about to do, which is the reason Codex keeps
 *    it in the action row instead of a settings page.
 *  - The card is a menu, not a dialog: `role="menu"` with `menuitemradio`
 *    rows, `aria-checked` on the live model, and a tick as the row's right
 *    icon. The heading is a section label inside the card, not a title bar.
 *  - Opening moves focus onto the checked row (`querySelector('[role=
 *    "menuitemradio"][aria-checked="true"]')?.focus({preventScroll:true})`)
 *    and scrolls the list back to the top, so a keyboard user lands on the
 *    current choice instead of the first one.
 *  - Arrow keys walk the rows, Home/End jump to the ends, Escape closes and
 *    returns focus to the trigger, Tab closes and lets the browser move on.
 *  - Rows are the "descriptive" menu variant: 44px tall, a 13px label with a
 *    12px tertiary sub-line under it, 6px card gutter and `radius - gutter`
 *    (14px) on the hover pill, list capped at 316px with its own scroll, and
 *    a footer split off by a hairline for the one action that is not a pick.
 *
 *  Two departures are ours, not Codex's: the card ends in a link to the
 *  registry (a pick can only switch, it cannot provision a model), and there
 *  is no reasoning-effort slider — our registry has aliases, not effort tiers,
 *  so the picker is the list view alone rather than Codex's two-view card. */
export function ModelSelect(props: { disabled?: boolean }) {
  const [open, setOpen] = useState(false)
  const [tick, setTick] = useState(0)
  const [busy, setBusy] = useState('')
  const { data } = useAsync(() => api.models(), [], tick)
  const box = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLDivElement>(null)
  const models: ModelEntry[] = data?.models ?? []
  const active = data?.active
  // The registry can carry a lone active model with an empty list — a node
  // that never registered an alias. The pick still has to name what is live,
  // so the active entry joins the rows when the list forgot it.
  const rows: ModelEntry[] = [
    ...(active && !models.some((m) => m.alias === active.alias) ? [active] : []),
    ...models,
  ]

  function close(restoreFocus: boolean) {
    setOpen(false)
    if (restoreFocus) trigger.current?.focus()
  }

  // Outside click closes the card; Escape closes it from anywhere and hands
  // focus back to the trigger, which is where a keyboard user left off.
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.stopPropagation()
      setOpen(false)
      trigger.current?.focus()
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey, true)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey, true)
    }
  }, [open])

  // Land on the live model, and start the list at its top — Codex does both,
  // and a card that always opens at row one hides the answer you came for.
  useEffect(() => {
    if (!open) return
    panel.current
      ?.querySelector<HTMLElement>('[role="menuitemradio"][aria-checked="true"]')
      ?.focus({ preventScroll: true })
    panel.current?.querySelector('.model-panel-list')?.scrollTo({ top: 0 })
  }, [open])

  /** The rows a keyboard can land on, in visual order. Disabled rows (one
   *  mid-switch) are skipped the way Codex skips `[data-disabled]`. */
  function menuItems(): HTMLElement[] {
    if (!panel.current) return []
    return Array.from(
      panel.current.querySelectorAll<HTMLElement>('[role="menuitemradio"]:not(:disabled)'),
    )
  }

  function onMenuKey(e: KeyboardEvent) {
    const items = menuItems()
    const at = items.indexOf(document.activeElement as HTMLElement)
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      if (items.length === 0) return
      const next = e.key === 'ArrowDown' ? Math.min(at + 1, items.length - 1) : Math.max(at - 1, 0)
      items[next]?.focus()
      return
    }
    if (e.key === 'Home' || e.key === 'End') {
      e.preventDefault()
      items[e.key === 'Home' ? 0 : items.length - 1]?.focus()
      return
    }
    // Tab is the browser's: the card just gets out of its way.
    if (e.key === 'Tab') setOpen(false)
  }

  async function pick(alias: string) {
    if (busy || alias === active?.alias) {
      close(true)
      return
    }
    setBusy(alias)
    try {
      await api.useModel(alias)
      toast(t('sessions.modelSwitched', { alias }))
      setOpen(false)
      trigger.current?.focus()
      // The list is the registry's: the active flag has to move with it.
      setTick((n) => n + 1)
    } catch (err) {
      toastError(err)
    } finally {
      setBusy('')
    }
  }

  return (
    <div class="model-select" ref={box}>
      <button
        ref={trigger}
        type="button"
        class={`model-select-btn${open ? ' open' : ''}`}
        /* The tip is suppressed while the card is up: it would sit on top of
           the card it just opened. Same rule as the node picker in the header. */
        data-tip={open ? undefined : t('sessions.modelBtn')}
        aria-label={`${t('sessions.modelBtn')}: ${active?.alias ?? ''}`}
        aria-haspopup="menu"
        aria-expanded={open}
        disabled={props.disabled || busy !== ''}
        onClick={() => (open ? close(false) : setOpen(true))}
      >
        <span class="model-select-content">
          <span class="model-select-name">{active?.alias ?? '—'}</span>
          <Icon name="chevron-down" size={16} class="model-select-arrow" />
        </span>
      </button>
      {open && (
        <div
          ref={panel}
          class="model-panel"
          role="menu"
          aria-label={t('sessions.modelBtn')}
          onKeyDown={onMenuKey}
        >
          {/* Codex heads the list with "Select model" — a section label, not
              a title bar — so the heading and the trigger's accessible name
              are the same sentence. */}
          <p class="model-panel-label">{t('sessions.modelBtn')}</p>
          <div class="model-panel-list">
            {rows.map((m) => (
              <button
                key={m.alias}
                type="button"
                role="menuitemradio"
                aria-checked={!!m.active}
                class={`model-row${m.active ? ' on' : ''}${busy === m.alias ? ' busy' : ''}`}
                disabled={busy === m.alias}
                onClick={() => void pick(m.alias)}
              >
                <span class="model-row-body">
                  <span class="model-row-alias">{m.alias}</span>
                  {/* What the name points at: an alias is a config, not a
                      model id, and the two routinely differ. */}
                  <span class="model-row-sub">
                    {m.provider_label || m.provider || m.api_type}
                    {' · '}
                    <span class="mono">{m.model || '—'}</span>
                  </span>
                </span>
                {m.active && <Icon name="check" size={16} class="model-row-check" />}
              </button>
            ))}
            {rows.length === 0 && <p class="model-panel-empty">{t('models.empty')}</p>}
          </div>
          {/* The pick is a switch, not a registry: adding a model, giving one
              a key or testing it lives in the models page, and this is the
              door to it. Codex splits its footer off the same way. */}
          <div class="model-panel-foot">
            <button
              type="button"
              class="model-foot-btn"
              onClick={() => {
                setOpen(false)
                navigate({ view: 'settings', tab: 'models' })
              }}
            >
              <Icon name="gear" size={15} />
              {t('sessions.modelManage')}
              <Icon name="chevron-right" size={14} class="model-foot-arrow" />
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
