// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useRef, useState } from 'preact/hooks'
import { api, type Task } from '../api/client'
import { useAsync, useChangeSignal } from '../hooks'
import { t } from '../i18n'
import { navigate } from '../nav'
import { layerAboveOwnsEscape } from './card-dismiss'
import { confirmDialog } from './confirm'
import { Icon } from './icons'
import DecisionOrbit from './orbit'
import { StateBadge } from './state-badge'
import { fmtElapsed, isActiveState } from './task-activity'
import { toast, toastError } from './toast'

/** The tasks this conversation started — the card behind the header's
 *  orchestration button.
 *
 *  Same shell as the node card beside it: ghost icon button, card hanging off
 *  the bar, the button toggles it and Escape closes it, and the pane reserves a
 *  lane on the right while it is up. The two answer the same kind of question
 *  about a turn — the node card says *where* it runs, this one says *what it
 *  became* — and both open in place so a running turn can be watched without
 *  leaving the transcript. Neither closes on an outside click: reading the
 *  thread under the card is the point, not a dismissal.
 *
 *  Three strips now: work that is moving (进行中), work parked on the human
 *  (待审核), and the archive (已完成 — done, failed and cancelled all landed
 *  somewhere, and none of them wants a decision). The card is tall enough for
 *  all three to be on screen at once, so the state of the thread is one glance
 *  rather than one scroll. A row's own actions (取消 / 执行) sit on the task,
 *  not behind a detail trip.
 *
 *  Above the strips sits the DecisionOrbit trace — the same card that used to
 *  ride above every task reply in the transcript. There it repeated itself for
 *  each task and pushed the answer down; here it is one rail, read when the
 *  question "how did this get routed / how far along is it" is actually asked,
 *  and it belongs to the task you are reading. It owns no click of its own:
 *  picking a task stays the chip's job, so the trace and the detail pane can
 *  never disagree about which task is on screen — one selection drives both.
 *
 *  The card reads top-down in the order the questions come: how is this turn
 *  being routed (trace), what has this thread produced (strips), what is in
 *  this one task (detail). The trace sits at the top because it is the one
 *  block whose height the reader controls: expanding it pushes the list down
 *  and the card scrolls, instead of squeezing the strips into a sliver the way
 *  it did when it sat between them and the detail pane. Every section keeps
 *  its full height whether or not it has rows in it (see `.orch-sec-slot`),
 *  so the three strips read as one table and the card does not jump when a
 *  task arrives or leaves.
 *
 *  A task opens *under the strips*, in the same card, and the strips stay where
 *  they are: the detail is a reading pane, not a page. Trading the rows for a
 *  detail view meant a back arrow for every look, and the whole point of
 *  reading tasks from here is that nothing has to be left behind. Clicking the
 *  same task again (or the × on the detail) puts it away. */
export function OrchestrateCard(props: {
  /** The thread whose tasks are shown; null before a new chat is saved. */
  sessionId?: string | null
  /** The other scope this card can answer for: a work directory. 工作空间
   *  dispatches into a directory rather than into a thread — it never shows a
   *  transcript, so "what has this directory produced" is the only question
   *  its header can usefully answer. Ignored when `sessionId` is set, so the
   *  chat's card keeps its exact behaviour. */
  project?: string
  /** Controlled by the pane: this card and the node card hang off the same
   *  corner, so opening one has to close the other. */
  open: boolean
  onOpenChange(open: boolean): void
  /** Node-directory facts for the trace's Step 2: how many machines can take
   *  this work decides whether it lists candidates or offers the "add a second
   *  device" CTA. Optional so the card still renders before the fleet loads. */
  onlineNodeCount?: number
  selfNodeId?: string
}) {
  const [picked, setPicked] = useState<string | null>(null)
  // The trace's steps. The reader owns this one: opening a task's detail no
  // longer folds it back. The two panes used to take turns because the card
  // had one slot of height for both of them; the card scrolls now, so folding
  // on a pick only made the layout jump under the cursor.
  const [traceOpen, setTraceOpen] = useState(false)
  // Local nudge after an action: the shared change feed usually arrives a beat
  // later, and the row the user just cancelled should not sit there looking
  // alive while they wait for it.
  const [bump, setBump] = useState(0)
  const [busy, setBusy] = useState('')
  const tick = useChangeSignal()
  const box = useRef<HTMLDivElement>(null)

  // Fetch only while the card is up: closed, there is nothing to paint, and
  // the header button carries no count that would need the data early.
  const { data, error } = useAsync(
    () => {
      if (!props.open) return Promise.resolve([] as Task[])
      if (props.sessionId) return api.tasks({ session: props.sessionId })
      // Directory scope: one read for every task filed under the project the
      // picked work dir resolves to, whatever state it is in.
      if (props.project) return api.tasks({ project: props.project })
      return Promise.resolve([] as Task[])
    },
    [props.open, props.sessionId, props.project],
    tick + bump,
  )
  const tasks = data ?? []
  const doing = tasks.filter((x) => isActiveState(x.state))
  const review = tasks.filter((x) => x.state === 'review')
  // Everything that is neither moving nor parked: done, failed, cancelled. One
  // strip, newest first — the archive is read top-down, not triaged.
  const finished = tasks
    .filter((x) => !isActiveState(x.state) && x.state !== 'review')
    .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at))

  // The newest task the thread produced: what the trace shows before anything
  // is picked, so opening the card answers "how is this turn going" without a
  // click. One selection drives everything under the strips — the trace and
  // the detail pane always describe the same task, which is what keeps the two
  // from fighting over the phrase "the task on screen".
  const newest = tasks.reduce<Task | null>(
    (a, b) => (!a || Date.parse(b.updated_at) >= Date.parse(a.updated_at) ? b : a),
    null,
  )
  const focusedId = picked ?? newest?.id ?? null

  // The full row (events, result, disposition, traces) for the focused task.
  // The list row paints immediately; this fills in behind it.
  const detail = useAsync(
    () => (focusedId ? api.task(focusedId) : Promise.resolve(null)),
    [focusedId],
    tick + bump,
  )
  const focused = focusedId
    ? detail.data && detail.data.id === focusedId
      ? detail.data
      : tasks.find((x) => x.id === focusedId) ?? null
    : null
  // A task that never went through the dispatcher has no classification, no
  // route and no execution trace to draw — the rail would be three empty steps
  // saying nothing at all. It shows up once the task has something to report.
  const traceTask = focused && hasTrace(focused) ? focused : null

  // Reopening starts at the top of the card: nothing picked, steps folded —
  // not with whatever was open last time.
  useEffect(() => {
    if (props.open) return
    setPicked(null)
    setTraceOpen(false)
  }, [props.open])

  // Opening a detail is a reading move, and the pane lands under the strips —
  // at the bottom of a card that scrolls as a whole, which is off screen
  // whenever the chip you clicked sits near the top. When the pane would not
  // fit under its chip, park the chip at the top of the viewport instead: the
  // chip and the pane then share one screen, and the selection stays marked
  // right above the text it opened.
  useEffect(() => {
    if (!picked) return
    const panel = box.current?.querySelector<HTMLElement>('.orch-panel')
    const chip = box.current?.querySelector<HTMLElement>('.orch-chip[data-picked]')
    const pane = box.current?.querySelector<HTMLElement>('.orch-detail')
    if (!panel || !chip || !pane) return
    const view = panel.getBoundingClientRect()
    // The panel's own padding — the top of the scrollport, not of the border.
    const top = view.top + 6
    if (pane.getBoundingClientRect().bottom <= view.bottom - 6) return
    panel.scrollTop += chip.getBoundingClientRect().top - top
  }, [picked])

  // Escape puts the detail away first, so one key undoes one step. It is the
  // card's only way out besides the header trigger: an outside click used to
  // close it too, which meant every click on a message — to select a line,
  // copy a path, follow a link — folded the card mid-read. The card is a
  // reading companion, not a menu; it stays until the button is pressed again.
  useEffect(() => {
    if (!props.open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || layerAboveOwnsEscape()) return
      e.stopPropagation()
      if (picked) setPicked(null)
      else props.onOpenChange(false)
    }
    document.addEventListener('keydown', onKey, true)
    return () => {
      document.removeEventListener('keydown', onKey, true)
    }
  }, [props.open, picked])

  /** Cancel or approve one task from the row that shows it. Both are
   *  destructive-ish, so cancel asks first (the run is killed); approve does
   *  not (it is the action the review row exists for). */
  async function act(task: Task, kind: 'cancel' | 'run') {
    if (kind === 'cancel') {
      const ok = await confirmDialog({
        title: t('detail.cancelConfirmTitle'),
        message: t('detail.cancelConfirmMsg'),
        confirmLabel: t('detail.cancelTask'),
        danger: true,
      })
      if (!ok) return
    }
    setBusy(`${task.id}:${kind}`)
    try {
      if (kind === 'cancel') {
        await api.cancel(task.id)
        toast(t('detail.cancelledToast'), 'success')
      } else {
        await api.approve(task.id)
        toast(t('detail.approvedToast'), 'success')
      }
      setBump((v) => v + 1)
    } catch (e) {
      toastError(e)
    } finally {
      setBusy('')
    }
  }

  const chipProps = (task: Task) => ({
    task,
    busy: busy.startsWith(`${task.id}:`),
    picked: picked === task.id,
    // Toggling on the same chip closes the pane: with the strips still up, the
    // chip is both the way in and the way out. The trace is left alone — it is
    // the reader's own control now that the card scrolls instead of trading
    // one pane for the other.
    onOpen: () => {
      setPicked((cur) => (cur === task.id ? null : task.id))
    },
    onCancel: () => void act(task, 'cancel'),
    onRun: () => void act(task, 'run'),
  })

  return (
    <div class="orch-picker" ref={box}>
      <button
        type="button"
        class={`icon-btn tip-down orch-pick-btn${props.open ? ' open' : ''}`}
        /* The tip is the control's name; the card behind it is the answer. */
        data-tip={props.open ? undefined : t('sessions.orchestrate')}
        aria-label={t('sessions.orchestrate')}
        aria-haspopup="dialog"
        aria-expanded={props.open}
        onClick={() => props.onOpenChange(!props.open)}
      >
        <Icon name="route" size={19} />
      </button>
      {props.open && (
        <div class="orch-panel" role="dialog" aria-label={t('sessions.orchestrate')}>
          {/* The trace first: it explains the task this card is focused on,
              and it is the one block whose height the reader controls — on top
              it pushes the list down (and the card scrolls) instead of eating
              the strips' room, which is what it did from between the strips
              and the detail pane. Collapsed by default: at 360px the summary
              line is what fits beside three strips, and the steps are one
              click away. It is a rail rather than a card in a card, so it
              drops its own plate inside the panel (see `.orch-trace .orbit`). */}
          {traceTask && (
            <section class="orch-trace" aria-label={t('orch.trace')}>
              <p class="orch-trace-head">
                <span class="orch-trace-label">{t('orch.trace')}</span>
                <span class="orch-trace-task" title={traceTask.title || traceTask.id}>
                  {traceTask.title || traceTask.id}
                </span>
              </p>
              <DecisionOrbit
                /* Remounted per task: the orbit keeps its own open/closed
                   state, and steps left expanded for the previous task must not
                   open the next one's. */
                key={traceTask.id}
                task={traceTask}
                onlineNodeCount={props.onlineNodeCount}
                selfNodeId={props.selfNodeId}
                open={traceOpen}
                onOpenChange={setTraceOpen}
              />
            </section>
          )}
          {/* The strips, under the trace. They keep their natural height —
              every section reserves a tile row even when it is empty, so the
              three read as one table — and the card scrolls as a whole when
              the trace and a detail pane are open at the same time. */}
          <div class="orch-body">
            <p class="orch-panel-label">
              {props.sessionId ? t('orch.title') : t('orch.projectTitle')}
            </p>
            {error ? (
              <p class="orch-sec-empty">{t('common.loadFail')}</p>
            ) : !props.sessionId && !props.project ? (
              <p class="orch-sec-empty">{t('orch.noSession')}</p>
            ) : (
              <>
                <OrchRow
                  title={t('orch.doing')}
                  tasks={doing}
                  empty={t('orch.emptyDoing')}
                  chipProps={chipProps}
                />
                <OrchRow
                  title={t('orch.review')}
                  tasks={review}
                  empty={t('orch.emptyReview')}
                  chipProps={chipProps}
                />
                <OrchRow
                  title={t('orch.done')}
                  tasks={finished}
                  empty={t('orch.emptyDone')}
                  chipProps={chipProps}
                />
              </>
            )}
          </div>
          {picked && focused && (
            <OrchDetail
              /* Keyed by task: switching tasks remounts the pane, so its
                 scroll starts at the top instead of at the last task's
                 offset. */
              key={focused.id}
              task={focused}
              busy={busy.startsWith(`${focused.id}:`)}
              onClose={() => setPicked(null)}
              onCancel={() => void act(focused, 'cancel')}
              onRun={() => void act(focused, 'run')}
            />
          )}
        </div>
      )}
    </div>
  )
}

/** One horizontal strip: a label with its count, then the tasks side by side.
 *  An empty strip keeps a tile-sized slot rather than collapsing to a line of
 *  text: the three strips are read as one table, and a section that shrank to
 *  its placeholder made the card jump every time a task arrived or left. */
function OrchRow(props: {
  title: string
  tasks: Task[]
  empty: string
  chipProps(task: Task): {
    task: Task
    busy: boolean
    picked: boolean
    onOpen(): void
    onCancel(): void
    onRun(): void
  }
}) {
  return (
    <section class="orch-sec">
      <p class="orch-sec-head">
        <span>{props.title}</span>
        <span class="orch-sec-count">{props.tasks.length}</span>
      </p>
      {props.tasks.length === 0 ? (
        <p class="orch-sec-slot">{props.empty}</p>
      ) : (
        <div class="orch-row">
          {props.tasks.map((task) => (
            <OrchChip key={task.id} {...props.chipProps(task)} />
          ))}
        </div>
      )}
    </section>
  )
}

/** A task as it appears in a row: title, state, age, and its one action.
 *  The open target is a real button (not the whole tile) so the action button
 *  beside it is not a control nested inside another control. */
function OrchChip(props: {
  task: Task
  busy: boolean
  picked: boolean
  onOpen(): void
  onCancel(): void
  onRun(): void
}) {
  const active = isActiveState(props.task.state)
  const blocked = props.task.approval_disposition === 'needs_changed_input'
  return (
    <div class="orch-chip" data-state={props.task.state} data-picked={props.picked || undefined}>
      <button
        type="button"
        class="orch-chip-open"
        aria-expanded={props.picked}
        aria-controls="orch-detail"
        onClick={props.onOpen}
      >
        <span class="orch-chip-title">{props.task.title || props.task.id}</span>
        <span class="orch-chip-meta">
          <StateBadge state={props.task.state} />
          <span class="orch-chip-age">{fmtElapsed(props.task.updated_at)}</span>
        </span>
      </button>
      <div class="orch-chip-acts">
        {active && (
          <button
            type="button"
            class="orch-act"
            disabled={props.busy}
            onClick={props.onCancel}
            title={t('detail.cancelTask')}
          >
            {t('orch.cancel')}
          </button>
        )}
        {props.task.state === 'review' && (
          <button
            type="button"
            class="orch-act run"
            disabled={props.busy || blocked}
            onClick={props.onRun}
            title={blocked ? t('detail.needsChangedInputHint') : t('detail.approve')}
          >
            {t('orch.run')}
          </button>
        )}
      </div>
    </div>
  )
}

/** The picked task, in a pane under the strips. Same actions as the row plus
 *  the way out to the full task page for anything this card cannot show. */
function OrchDetail(props: {
  task: Task
  busy: boolean
  onClose(): void
  onCancel(): void
  onRun(): void
}) {
  const task = props.task
  const active = isActiveState(task.state)
  const review = task.state === 'review'
  const blocked = task.approval_disposition === 'needs_changed_input'
  const brief = task.spec || task.intent || ''
  return (
    <div class="orch-detail" id="orch-detail" role="region" aria-label={t('orch.detail')}>
      <div class="orch-detail-head">
        <p class="orch-detail-title">{task.title || task.id}</p>
        <button
          type="button"
          class="icon-btn orch-detail-close"
          aria-label={t('orch.closeDetail')}
          title={t('orch.closeDetail')}
          onClick={props.onClose}
        >
          <Icon name="x" size={14} />
        </button>
      </div>
      <div class="orch-detail-meta">
        <StateBadge state={task.state} />
        {task.project && <span class="orch-detail-tag">{task.project}</span>}
        {task.owner && <span class="orch-detail-tag">@{task.owner}</span>}
        <span class="orch-detail-age">{t('time.ago', { d: fmtElapsed(task.updated_at) })}</span>
      </div>
      {review && task.approval_disposition && (
        <p class={`orch-detail-note${blocked ? ' blocked' : ''}`}>
          {t(`detail.reviewKind.${task.approval_disposition}`)}
        </p>
      )}
      {brief && (
        <div class="orch-detail-sec">
          <p class="orch-detail-label">{t('orch.brief')}</p>
          <p class="orch-detail-text">{brief}</p>
        </div>
      )}
      <div class="orch-detail-sec">
        <p class="orch-detail-label">{t('orch.result')}</p>
        {task.result ? (
          <pre class="orch-detail-pre">{task.result}</pre>
        ) : (
          <p class="orch-detail-text dim">{t('orch.noResult')}</p>
        )}
      </div>
      <div class="orch-detail-foot">
        {active && (
          <button class="btn small" disabled={props.busy} onClick={props.onCancel}>
            {t('orch.cancel')}
          </button>
        )}
        {review && (
          <button
            class="btn primary small"
            disabled={props.busy || blocked}
            title={blocked ? t('detail.needsChangedInputHint') : undefined}
            onClick={props.onRun}
          >
            {t('orch.run')}
          </button>
        )}
        <button
          class="btn ghost small"
          onClick={() => navigate({ view: 'detail', id: task.id })}
        >
          {t('orch.openTask')}
        </button>
      </div>
    </div>
  )
}

/** Whether there is an orchestration story to tell for this task. A task that
 *  was dispatched straight to an agent — or is still being created — has no
 *  classification, no route decision and no execution trace, and the orbit
 *  would render three empty steps under a "大总管" bar that did nothing. */
function hasTrace(task: Task): boolean {
  return Boolean(task.plan_meta) || (task.traces?.length ?? 0) > 0 || Boolean(task.last_event)
}
