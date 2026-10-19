// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useRef, useState } from 'preact/hooks'
import { api, type DirectoryListing } from '../api/client'
import { t } from '../i18n'
import { Icon } from './icons'
import { lastPathSegment, suggestProjectName } from './dir-utils'
import { useModalFocus } from './modal-focus'

export interface DirPickerProps {
  value?: string
  onChange: (path: string) => void
  onSuggestName?: (name: string) => void
  placeholder?: string
  disabled?: boolean
}

/** The pick itself: the native chooser first, the in-page browser as the
 *  fallback for a node whose panel cannot raise one. Shared by the boxed
 *  picker on the projects page and the compact button the composer's
 *  new-chat footer uses — the two look nothing alike but must fail alike. */
function useDirPick(onPicked: (path: string) => void, disabled: boolean) {
  const [modalOpen, setModalOpen] = useState(false)
  const [loading, setLoading] = useState(false)

  async function open(current: string) {
    if (disabled || loading) return
    setLoading(true)
    try {
      // The caption is ours to write: the panel serves five languages and
      // cannot tell which one the browser is in.
      const res = await api.chooseDirectory(current, t('sessions.chooseDir'))
      const canceled = !!(res && (res.canceled ?? res.cancelled))
      if (res && res.path && !canceled) {
        onPicked(res.path)
        return
      }
      // A cancel is an answer — the user looked and changed their mind. Do not
      // answer it by opening a second picker over the one they just closed.
      if (canceled) return
      // Otherwise no window can be raised for this console (a browser on
      // another device, a host with no desktop shell) or it failed to open:
      // fall back to the in-page browser, which always works.
      setModalOpen(true)
    } catch {
      setModalOpen(true)
    } finally {
      setLoading(false)
    }
  }

  return { modalOpen, setModalOpen, loading, open }
}

export function DirPicker({
  value = '',
  onChange,
  onSuggestName,
  placeholder,
  disabled = false,
}: DirPickerProps) {
  const pick = useDirPick((path) => {
    onChange(path)
    if (onSuggestName) {
      onSuggestName(suggestProjectName(path))
    }
  }, disabled)
  const { modalOpen, setModalOpen, loading } = pick

  function handlePick() {
    void pick.open(value)
  }

  return (
    <div class="dir-picker-wrap">
      <div class={`dir-picker-box ${value ? 'has-value' : ''} ${disabled ? 'disabled' : ''}`}>
        <Icon name="folder" class="dir-picker-icon" />
        <span class="dir-picker-path" title={value || placeholder || t('projects.dirPlaceholder')}>
          {value || <span class="dim">{placeholder || t('projects.dirPlaceholder')}</span>}
        </span>
        {value && !disabled && (
          <button
            type="button"
            class="dir-picker-clear"
            onClick={(e) => {
              e.stopPropagation()
              onChange('')
            }}
            title={t('common.clear')}
          >
            <Icon name="x" size={12} />
          </button>
        )}
        <button
          type="button"
          class="btn dir-picker-browse-btn"
          onClick={handlePick}
          disabled={disabled || loading}
        >
          {loading ? t('common.loading') : t('projects.chooseDir')}
        </button>
      </div>

      {modalOpen && (
        <DirBrowserModal
          initialPath={value}
          onSelect={(selectedPath) => {
            onChange(selectedPath)
            if (onSuggestName) {
              onSuggestName(suggestProjectName(selectedPath))
            }
            setModalOpen(false)
          }}
          onClose={() => setModalOpen(false)}
        />
      )}
    </div>
  )
}

/** The compact pick: a folder glyph and the chosen path set in the same small
 *  print as the composer's keyboard legend. The projects page needs a form
 *  field; a new thread only needs to say where it will run, so this one is a
 *  footnote that happens to be clickable.
 *
 *  A chosen directory is also undoable, and the bubble is where that lives:
 *  under the pointer the glyph turns into a cross, and the cross drops the
 *  pick back to "choose a directory". The glyph is the cross's slot rather
 *  than a second control beside the name — the chip is one line of small
 *  print, and an × parked next to a long folder name would either crowd it or
 *  be pushed out of the clamp. */
export function DirButton({
  value = '',
  onChange,
  label,
  disabled = false,
}: {
  value?: string
  onChange: (path: string) => void
  /** The picker's own name, shown while nothing is chosen. */
  label: string
  disabled?: boolean
}) {
  const pick = useDirPick(onChange, disabled)
  const clearable = !!value && !disabled
  return (
    <>
      <div
        class={`workdir-btn${value ? ' has-value' : ''}${disabled ? ' is-disabled' : ''}`}
        /* The bubble is where the whole path lives: the label shows the folder
           on its own, so hovering is how you check *which* folder. Nothing to
           reveal while the pick is still empty — the label already says it. */
        data-tip={value || undefined}
        /* The bubble is also the only place the cross can be announced: it is
           invisible until the pointer is on the chip, so hovering is the one
           moment the affordance needs a name. */
        data-tip-hint={clearable ? t('sessions.clearDirHint') : undefined}
      >
        <button
          type="button"
          class="workdir-open"
          aria-label={value ? `${label}: ${value}` : label}
          disabled={disabled || pick.loading}
          onClick={() => void pick.open(value)}
        >
          {/* The rail's working-directory mark, at the rail's own size: the
              header's closed folder would read as a second title down here. */}
          <Icon name="folder-open" size={13} class="workdir-glyph" />
          <span class="workdir-path">{value ? lastPathSegment(value) : label}</span>
        </button>
        {clearable && (
          <button
            type="button"
            class="workdir-clear"
            aria-label={t('sessions.clearDir')}
            onClick={() => onChange('')}
          >
            <Icon name="x" size={13} />
          </button>
        )}
      </div>
      {pick.modalOpen && (
        <DirBrowserModal
          initialPath={value}
          onSelect={(selectedPath) => {
            onChange(selectedPath)
            pick.setModalOpen(false)
          }}
          onClose={() => pick.setModalOpen(false)}
        />
      )}
    </>
  )
}

function DirBrowserModal({
  initialPath,
  onSelect,
  onClose,
}: {
  initialPath?: string
  onSelect: (path: string) => void
  onClose: () => void
}) {
  const [currentPath, setCurrentPath] = useState(initialPath || '')
  const [listing, setListing] = useState<DirectoryListing | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const box = useRef<HTMLDivElement>(null)
  useModalFocus(box, true)

  async function loadPath(path: string) {
    setLoading(true)
    setError('')
    try {
      const res = await api.listDirectories(path)
      setListing(res)
      setCurrentPath(res.current)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadPath(currentPath)
  }, [])

  const parts = currentPath ? currentPath.split(/[/\\]/).filter(Boolean) : []
  const isWindows = listing?.separator === '\\'

  return (
    <div
      class="modal-backdrop"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div
        ref={box}
        class="modal dir-browser-modal"
        role="dialog"
        aria-modal="true"
        aria-label={t('projects.selectDirTitle')}
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            e.stopPropagation()
            onClose()
          }
        }}
      >
        <div class="dir-browser-header">
          <h2 class="modal-title">{t('projects.selectDirTitle')}</h2>
          <button type="button" class="btn-icon" onClick={onClose} title={t('common.close')}>
            ✕
          </button>
        </div>

        <div class="dir-breadcrumbs">
          <button
            type="button"
            class="dir-crumb-btn"
            onClick={() => loadPath(isWindows ? 'C:\\' : '/')}
          >
            {isWindows ? 'C:\\' : '/'}
          </button>
          {parts.map((p, i) => {
            // On Windows parts[0] is the drive ("C:") — a bare "C:" resolves
            // to the process cwd on that drive, not the drive root, so it
            // needs its trailing separator.
            const pathUpTo = isWindows && i === 0
              ? parts[0] + '\\'
              : (isWindows ? '' : '/') + parts.slice(0, i + 1).join(isWindows ? '\\' : '/')
            return (
              <span key={i} class="dir-crumb-item">
                <span class="dir-crumb-sep">/</span>
                <button
                  type="button"
                  class={`dir-crumb-btn ${i === parts.length - 1 ? 'active' : ''}`}
                  onClick={() => loadPath(pathUpTo)}
                >
                  {p}
                </button>
              </span>
            )
          })}
        </div>

        {listing?.parent && listing.parent !== listing.current && (
          <div class="dir-up-row">
            <button
              type="button"
              class="dir-entry-btn up"
              onClick={() => loadPath(listing.parent)}
            >
              <Icon name="arrow-up" class="dir-icon" />
              <span>.. ({t('projects.parentDir')})</span>
            </button>
          </div>
        )}

        <div class="dir-entries-list">
          {loading ? (
            <div class="dim dir-loading">
              <span class="spinner spinner-inline" aria-hidden="true" />
              {t('common.loading')}
            </div>
          ) : error ? (
            <div class="dir-error">{error}</div>
          ) : listing?.directories && listing.directories.length > 0 ? (
            listing.directories.map((entry) => (
              <button
                key={entry.path}
                type="button"
                class="dir-entry-btn"
                onClick={() => loadPath(entry.path)}
              >
                <Icon name="folder" class="dir-icon" />
                <span class="dir-entry-name">{entry.name}</span>
              </button>
            ))
          ) : (
            <div class="dim dir-empty">{t('projects.noSubdirs')}</div>
          )}
        </div>

        <div class="dir-browser-footer">
          <div class="dir-current-selected">
            <span class="dim">{t('projects.currentPath')}: </span>
            <code>{currentPath || '/'}</code>
          </div>
          <div class="modal-actions">
            <button type="button" class="btn" onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button
              type="button"
              class="btn primary"
              disabled={!currentPath}
              onClick={() => onSelect(currentPath)}
            >
              {t('projects.confirmSelect')}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
