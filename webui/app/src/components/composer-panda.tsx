// SPDX-License-Identifier: AGPL-3.0-or-later

import { useState } from 'preact/hooks'
import { t } from '../i18n'

/* The pet that stands on the composer's top-left corner.
 *
 * It used to be a row of its own above the box — a 56px sprite plus a dot and
 * the word "待命" — which cost the new-chat page a line of empty canvas and
 * read as a status readout for a thread that does not exist yet. The pose is
 * the readout now: it stands when the console is free and cheers while a turn
 * streams. The words stay for screen readers, so the live region still says
 * what the sprite shows.
 *
 * Pressing it used to lay the pet down, and a second press stood it back up.
 * It is a toy instead: a press squashes it flat and lets it ring back up, the
 * way a jelly toy answers a poke. That is the whole interaction — no state to
 * remember, no second press to undo the first, nothing on the page changes but
 * the sprite. */
export function ComposerPanda({ working }: { working: boolean }) {
  /* A counter, not a flag: the animation has to replay on every press, and
     bumping the sprite's `key` remounts it, which is the only reliable way to
     restart a CSS animation that is already on the element (re-adding the same
     class does nothing). It returns to 0 when the squish ends, so the next
     press starts from a clean element. */
  const [bounce, setBounce] = useState(0)
  const bouncing = bounce > 0

  return (
    <button
      type="button"
      class={'composer-panda' + (working ? ' is-working' : '') + (bouncing ? ' is-bouncing' : '')}
      /* Named for what a press does. "Ready" on a button tells you nothing
         about the press; the state is the live region's job, below.
         No `title`: the pet is decoration first, and a native tooltip popped
         a grey bubble over the composer's top-left corner on every pass of
         the pointer. The name still reaches assistive tech through
         `aria-label`, so nothing is lost but the bubble. */
      aria-label={t('panda.poke')}
      onClick={() => {
        // While a turn streams the pet is busy cheering, and a squish would
        // fight the working animation — a press there is ignored rather than
        // queued for the moment the turn ends.
        if (working) return
        setBounce((n) => n + 1)
      }}
    >
      <span class="composer-panda-stage" aria-hidden="true">
        <span
          key={bounce}
          class="composer-panda-sprite"
          onAnimationEnd={() => setBounce(0)}
        />
      </span>
      <span class="composer-panda-label" role="status" aria-live="polite" aria-atomic="true">
        {t(working ? 'panda.working' : 'panda.ready')}
      </span>
    </button>
  )
}
