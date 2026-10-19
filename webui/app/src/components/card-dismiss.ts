// SPDX-License-Identifier: AGPL-3.0-or-later

/** Dismissal rules for the two cards that hang off the chat header (the node
 *  card and the orchestration card).
 *
 *  They are not menus and they do not close on an outside click. Both are read
 *  *while* the thread is being used — you open one to pin a machine or watch a
 *  task, then keep reading the answer behind it, select a line, follow a link.
 *  Every click in the transcript used to put the card away, which meant the
 *  panel disappeared at the first sign of reading, and the rule had to grow a
 *  list of exceptions for the composer, then a second one for the popovers the
 *  composer grows. The trigger in the header is now the only click that closes
 *  a card; Escape is the keyboard equivalent. */

/** Whether a layer above the header card owns Escape right now.
 *
 *  Both cards listen for Escape in the capture phase and stop the event, so
 *  without this check one key press closed the card *and* left the thing it
 *  menu — the card has already stopped the event on its way down — or reset
 *  the task detail instead of closing the card. Escape closes the topmost
 *  layer: a menu hanging off the composer, or a modal, takes it first; the
 *  card takes the next press.
 *
 *  A DOM query rather than a shared store because these popovers are three
 *  different components that already own their own state — the alternative was
 *  threading an "is any menu open" flag through the pane and every menu. */
export function layerAboveOwnsEscape(): boolean {
  return Boolean(
    document.querySelector('.model-panel, .attach-menu, .complete-menu, .modal-backdrop'),
  )
}
