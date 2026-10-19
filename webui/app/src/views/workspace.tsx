// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { api, askSessionStream, type NodeInfo, type SelfInfo, type Session } from '../api/client'
import { useAsync, useChangeSignal, useLocaleRerender, useVisibleInterval } from '../hooks'
import { t } from '../i18n'
import { ErrorState } from '../components/page'
import { Icon } from '../components/icons'
import { toast, toastError } from '../components/toast'
import { DirButton } from '../components/dir-picker'
import { ModelSelect } from '../components/model-select'
import { suggestProjectName } from '../components/dir-utils'
import { expandFileRefs } from '../components/attach'
import { OrchestrateCard } from '../components/orchestrate'
import { layerAboveOwnsEscape } from '../components/card-dismiss'
import { SessionPick } from '../components/session-pick'
import { autoGrow, editorClear, editorPlain, editorTextOf, range_or_append, useBrParagraphSeparator } from './sessions'

/** 工作空间 — the fleet as a map. 节点网络 answers "what can each device do"
 *  with cards and forms; this page answers the prior question, "what is
 *  connected to what, and what kind of machine is on the other end", as a
 *  topology: one plate per device, one line per live adjacency, the glyph
 *  drawn from the form factor the machine advertises about itself.
 *
 *  The graph is dependency-free SVG, like the memory map: a deterministic
 *  ring layout (distance from this console = ring index) plus pan/zoom. No
 *  force simulation — a mesh this small reads better when the same fleet
 *  always lands in the same picture. */

type Form = 'laptop' | 'desktop' | 'mini' | 'server' | 'vm' | 'unknown'

const FORMS: Form[] = ['laptop', 'desktop', 'mini', 'server', 'vm', 'unknown']

interface Device {
  id: string
  name: string
  form: Form
  /** Raw GOOS ("darwin"/"windows"/"linux"/…); empty when unreported. */
  os: string
  online: boolean
  local: boolean
  chip: string
  cores: number
  ram: number
  tasks: number
  maxTasks: number
  rtt: number
  transport: string
  trust: string
  ver: string
  lastSeen: string
  neighbors: string[]
}

/** One line on the map. `mesh` is a link both ends advertise (a real
 *  adjacency in the routing graph); `measured` is an RTT this console
 *  measured toward a peer that did not advertise us back — still a live
 *  edge, drawn lighter. */
interface Edge {
  a: string
  b: string
  kind: 'mesh' | 'measured'
  rtt: number
  transport: string
}

interface View {
  k: number
  x: number
  y: number
}

const VIEW_HOME: View = { k: 1, x: 0, y: 0 }
const ZOOM_MIN = 0.5
const ZOOM_MAX = 3

/** Plate edge in graph units, the label block under it, and the breathing
 *  room two neighbouring plates need. The ring radii are derived from these
 *  so a crowded fleet never draws plates on top of each other. */
const PLATE = 56
const LABEL_H = 32
const NODE_GAP = 30

/** Advertised form factors normalize into the six glyph classes. Unknown
 *  spellings (a future class from a newer peer) fall back to `unknown`
 *  rather than being mistaken for a desktop. */
function normalizeForm(raw: string | undefined): Form {
  switch ((raw ?? '').toLowerCase()) {
    case 'laptop':
    case 'notebook':
    case 'portable':
      return 'laptop'
    case 'desktop':
    case 'tower':
    case 'imac':
    case 'workstation':
      return 'desktop'
    case 'mini':
    case 'macmini':
    case 'mac-mini':
    case 'nuc':
      return 'mini'
    case 'server':
    case 'rack':
      return 'server'
    case 'vm':
    case 'virtual':
      return 'vm'
    default:
      return 'unknown'
  }
}

/** Last-resort hint for a peer whose row predates the form-factor field: the
 *  hostname sometimes names the family. Only ever used when the node itself
 *  said nothing — a reported class always wins. */
function guessForm(name: string, os: string): Form {
  const n = name.toLowerCase()
  if (/macbook|laptop|notebook|thinkpad|book pro|book air/.test(n)) return 'laptop'
  if (/mac ?mini|\bmini\b|\bnuc\b/.test(n)) return 'mini'
  if (/server|rack|nas/.test(n)) return 'server'
  if (os === 'darwin') return 'desktop'
  return 'unknown'
}

function osLabel(os: string): string {
  switch (os) {
    case 'darwin':
      return t('workspace.os.mac')
    case 'windows':
      return t('workspace.os.windows')
    case 'linux':
      return t('workspace.os.linux')
    default:
      return t('workspace.os.unknown')
  }
}

function formLabel(form: Form): string {
  return t(`workspace.form.${form}`)
}

function isLocal(self: SelfInfo | null, n: NodeInfo): boolean {
  if (n.is_local) return true
  if (!self) return false
  if (self.node_id && self.node_id === n.id) return true
  if (self.node && self.node.id === n.id) return true
  const name = self.node_name || self.hostname
  return name !== '' && (n.name === name || n.id === name)
}

function buildDevices(nodes: NodeInfo[], self: SelfInfo | null): Device[] {
  const devices = nodes
    .map((n) => {
      const local = isLocal(self, n)
      const os = (local && self ? self.os : n.os) || ''
      const name = n.name || n.id
      const reported = normalizeForm(n.form_factor)
      return {
        id: n.id,
        name,
        form: reported !== 'unknown' ? reported : guessForm(name, os),
        os,
        // The row's status is the directory's view; `running` adds "this
        // process is up right now" for the local row only.
        online: n.status === 'online',
        local,
        chip: (local && self ? self.chip : '') || n.chip || '',
        cores: n.capacity?.cpu_cores ?? (local && self ? self.cpu_cores : 0),
        ram: n.capacity?.ram_gb ?? (local && self ? self.ram_gb ?? 0 : 0),
        tasks: n.capacity?.current_tasks ?? 0,
        maxTasks: n.capacity?.max_concurrent_tasks ?? 0,
        rtt: n.rtt_ms ?? 0,
        transport: n.transport ?? '',
        trust: n.trust ?? '',
        ver: n.ver ?? '',
        lastSeen: n.last_seen,
        neighbors: n.neighbors ?? [],
      } satisfies Device
    })
    .sort((a, b) => {
      if (a.local !== b.local) return a.local ? -1 : 1
      if (a.online !== b.online) return a.online ? -1 : 1
      return a.name.localeCompare(b.name)
    })
  // The map always starts from here. A store the node has not registered in
  // yet (a fresh database, a console that came up before the first heartbeat)
  // has no rows at all, and an empty canvas is a worse answer than "this is
  // the machine you are on" — the one device the console can always name.
  if (self && !devices.some((d) => d.local)) {
    const name = self.node_name || self.hostname || t('workspace.local')
    const form = normalizeForm(self.form_factor)
    devices.unshift({
      id: self.node_id || name,
      name,
      form: form !== 'unknown' ? form : guessForm(name, self.os),
      os: self.os,
      online: true,
      local: true,
      chip: self.chip ?? '',
      cores: self.cpu_cores,
      ram: self.ram_gb ?? 0,
      tasks: 0,
      maxTasks: 0,
      rtt: 0,
      transport: '',
      trust: '',
      ver: self.version ?? '',
      lastSeen: new Date().toISOString(),
      neighbors: [],
    })
  }
  return devices
}

function buildEdges(devices: Device[]): Edge[] {
  const known = new Map(devices.map((d) => [d.id, d]))
  const out = new Map<string, Edge>()
  const key = (a: string, b: string) => [a, b].sort().join('\u0000')
  const put = (a: string, b: string, kind: Edge['kind'], rtt: number, transport: string) => {
    const k = key(a, b)
    const prev = out.get(k)
    if (!prev) {
      out.set(k, { a, b, kind, rtt, transport })
      return
    }
    // Both ends agreeing is the stronger claim; a measurement only ever adds
    // the label to a line that already exists.
    if (kind === 'mesh') prev.kind = 'mesh'
    if (rtt > 0 && prev.rtt === 0) {
      prev.rtt = rtt
      prev.transport = transport
    }
  }
  for (const d of devices) {
    for (const nb of d.neighbors) {
      if (nb === d.id || !known.has(nb)) continue
      put(d.id, nb, 'mesh', 0, '')
    }
  }
  // This console's own RTT samples: an edge it measured is live even when
  // the peer never advertised it back (one-way visibility, older build).
  const local = devices.find((d) => d.local)
  if (local) {
    for (const d of devices) {
      if (d.id === local.id || d.rtt <= 0) continue
      put(local.id, d.id, 'measured', d.rtt, d.transport)
    }
  }
  return [...out.values()]
}

/** Ring index = hop distance from this console over the drawn edge set.
 *  Disconnected rows (a directory entry with no live peer) land on the
 *  outermost ring, where "not connected" is visible as distance. */
function ringDepths(devices: Device[], edges: Edge[]): { depth: Map<string, number>; max: number } {
  const adj = new Map<string, string[]>()
  const link = (a: string, b: string) => {
    const list = adj.get(a) ?? []
    list.push(b)
    adj.set(a, list)
  }
  for (const e of edges) {
    link(e.a, e.b)
    link(e.b, e.a)
  }
  const depth = new Map<string, number>()
  const root = devices.find((d) => d.local) ?? devices[0]
  if (!root) return { depth, max: 0 }
  depth.set(root.id, 0)
  const queue = [root.id]
  let max = 0
  while (queue.length > 0) {
    const cur = queue.shift()!
    const d = depth.get(cur) ?? 0
    for (const nb of adj.get(cur) ?? []) {
      if (depth.has(nb)) continue
      depth.set(nb, d + 1)
      max = Math.max(max, d + 1)
      queue.push(nb)
    }
  }
  // Unreached rows go one ring past the deepest reachable one.
  const orphan = devices.some((d) => !depth.has(d.id)) ? max + 1 : max
  for (const d of devices) if (!depth.has(d.id)) depth.set(d.id, orphan)
  return { depth, max: orphan }
}

interface Placed {
  device: Device
  x: number
  y: number
}

function layout(devices: Device[], depth: Map<string, number>, w: number, h: number): Placed[] {
  const cx = w / 2
  const cy = h / 2
  // The outermost ring is bounded by the canvas: half the short side, minus
  // the plate and the label block under it.
  const maxR = Math.max(
    90,
    Math.min(w / 2 - PLATE / 2 - 16, h / 2 - PLATE / 2 - LABEL_H - 22),
  )
  // Two plates on adjacent rings need a full plate + label between them, or
  // the outer one's plate covers the inner one's name. When the canvas cannot
  // pay for one ring per hop, deeper nodes collapse onto the outermost ring
  // (their exact hop count still shows in the rail's detail card).
  const minStep = PLATE + LABEL_H
  const rawMax = Math.max(1, ...devices.map((d) => depth.get(d.id) ?? 0))
  const maxRing = rawMax > 1 ? Math.max(1, Math.min(rawMax, Math.floor(maxR / minStep))) : 1
  const ringOf = (d: Device) => Math.min(depth.get(d.id) ?? 0, maxRing)

  const byRing = new Map<number, Device[]>()
  for (const d of devices) {
    const r = ringOf(d)
    const list = byRing.get(r) ?? []
    list.push(d)
    byRing.set(r, list)
  }
  // Equal radial steps: the gap between rings is what keeps labels readable,
  // so it must not be squeezed by an inner-ring offset.
  const radiusOf = (ring: number) => (maxRing === 1 ? maxR : (maxR * ring) / maxRing)
  const out: Placed[] = []
  for (const [ring, list] of byRing) {
    const sorted = [...list].sort((a, b) => a.name.localeCompare(b.name))
    if (ring === 0) {
      out.push({ device: sorted[0]!, x: cx, y: cy })
      continue
    }
    // A ring must also be long enough to hold its own plates: circumference
    // ≥ n × (plate + gap). With one node on it, the geometric radius wins.
    const needed = (sorted.length * (PLATE + NODE_GAP)) / (2 * Math.PI)
    const radius = Math.max(radiusOf(ring), sorted.length > 1 ? needed : 0)
    // Rings are ellipses: the canvas is wider than it is tall, and a circle
    // would leave two thirds of the map empty on either side.
    const rx = Math.min(w / 2 - PLATE / 2 - 20, radius * 1.55)
    sorted.forEach((d, i) => {
      // Half-step offset per ring so a ring's plates never sit exactly on
      // the spokes of the one inside it.
      const angle = -Math.PI / 2 + ((i + 0.5) / sorted.length) * Math.PI * 2 + (ring % 2 ? 0.22 : 0)
      out.push({ device: d, x: cx + Math.cos(angle) * rx, y: cy + Math.sin(angle) * radius })
    })
  }
  return centerPlaced(out, w, h)
}

/** Shift the whole drawing so its ink — plates plus the label block under
 *  them — is centred in the canvas. Rings are concentric around this
 *  console, but the deepest ring is rarely symmetric, so the hub alone is
 *  not the visual centre. The shift is clamped to the canvas so a wide
 *  fleet can never push a plate off the edge. */
function centerPlaced(placed: Placed[], w: number, h: number): Placed[] {
  if (placed.length < 2) return placed
  let minX = Infinity
  let maxX = -Infinity
  let minY = Infinity
  let maxY = -Infinity
  for (const p of placed) {
    // Rough text metrics: 6.6px per character at the label's 12px, and the
    // os line is the widest fixed part of the block.
    const half = Math.max(PLATE / 2, p.device.name.length * 3.6, 40)
    minX = Math.min(minX, p.x - half)
    maxX = Math.max(maxX, p.x + half)
    minY = Math.min(minY, p.y - PLATE / 2 - 6)
    maxY = Math.max(maxY, p.y + PLATE / 2 + LABEL_H)
  }
  const dx = clampShift(w / 2 - (minX + maxX) / 2, 10 - minX, w - 10 - maxX)
  const dy = clampShift(h / 2 - (minY + maxY) / 2, 10 - minY, h - 10 - maxY)
  return placed.map((p) => ({ ...p, x: p.x + dx, y: p.y + dy }))
}

/** Clamp a desired shift into [lo, hi]; when the ink is wider than the
 *  canvas (lo > hi) the low bound wins so the leading edge stays visible. */
function clampShift(want: number, lo: number, hi: number): number {
  if (lo > hi) return lo
  return Math.min(hi, Math.max(lo, want))
}

/** A device glyph in a 24×24 box centered on the origin — screen+base for a
 *  laptop, a tower for a desktop, a low wide box for a mini, a rack for a
 *  server, a dashed outline for a guest. Stroke only, so the theme's ink
 *  color carries through (the plates are monochrome by design: hue is
 *  reserved for status). */
function DeviceGlyph({ form }: { form: Form }) {
  switch (form) {
    case 'laptop':
      return (
        <>
          <rect x={-9} y={-8} width={18} height={12} rx={1.8} />
          <path d="M-12 6.4 H12 L10 9 H-10 Z" />
        </>
      )
    case 'desktop':
      return (
        <>
          <rect x={-7.5} y={-10.5} width={15} height={21} rx={2.4} />
          <path d="M-4.2 -6.8 h8.4" />
          <path d="M-4.2 -3.8 h8.4" />
          <circle cx={-4} cy={6.4} r={1.15} />
        </>
      )
    case 'mini':
      return (
        <>
          <rect x={-10.5} y={-5.4} width={21} height={10.8} rx={3} />
          <path d="M-10.5 -1.8 H10.5" opacity={0.45} />
          <circle cx={6.6} cy={2.6} r={0.95} />
        </>
      )
    case 'server':
      return (
        <>
          <rect x={-8.5} y={-9.5} width={17} height={5.4} rx={1.4} />
          <rect x={-8.5} y={-2.7} width={17} height={5.4} rx={1.4} />
          <rect x={-8.5} y={4.1} width={17} height={5.4} rx={1.4} />
          <circle cx={5} cy={-6.8} r={0.9} />
          <circle cx={5} cy={0} r={0.9} />
          <circle cx={5} cy={6.8} r={0.9} />
        </>
      )
    case 'vm':
      return (
        <>
          <rect x={-9.5} y={-8.5} width={19} height={17} rx={3} stroke-dasharray="3 2.6" />
          <rect x={-4.6} y={-3.6} width={9.2} height={7.2} rx={1.4} />
          <path d="M-9.5 1.5 H-6.4 M6.4 1.5 H9.5" opacity={0.4} />
        </>
      )
    default:
      return (
        <>
          <rect x={-8.5} y={-8.5} width={17} height={17} rx={3.2} stroke-dasharray="2.6 2.6" />
          <text
            x={0}
            y={3.6}
            text-anchor="middle"
            font-size="10"
            font-weight="600"
            fill="currentColor"
            stroke="none"
          >
            ?
          </text>
        </>
      )
  }
}

export function WorkspaceView() {
  useLocaleRerender()
  const change = useChangeSignal()
  const [tick, setTick] = useState(0)
  const { data: nodes, error } = useAsync(() => api.nodes(), [], change + tick)
  const { data: self } = useAsync(() => api.self(), [], tick)
  const [selected, setSelected] = useState<string | null>(null)
  const [hover, setHover] = useState<string | null>(null)
  const [view, setView] = useState<View>(VIEW_HOME)
  // The rail is a reading panel, not part of the map: it starts folded so the
  // drawing owns the pane, and opens when the reader asks for it — either by
  // the header toggle or by picking a device, whose facts only live in there.
  const [railOpen, setRailOpen] = useState(false)
  const [size, setSize] = useState({ w: 860, h: 520 })
  const [panning, setPanning] = useState(false)
  const wrap = useRef<HTMLDivElement>(null)
  const svgRef = useRef<SVGSVGElement>(null)
  const drag = useRef<{ px: number; py: number; vx: number; vy: number } | null>(null)

  // The bench's own state: where the next task runs, and which thread of that
  // directory it continues (null = mint a new one). It lives up here because
  // the header's orchestration card needs the resolved project — the card
  // answers "what has this directory produced", which is the only feedback
  // this page has, since it never paints a transcript.
  const [pickedDir, setPickedDir] = useState('')
  const [dirTouched, setDirTouched] = useState(false)
  const [pickedSession, setPickedSession] = useState<Session | null>(null)
  const [orchOpen, setOrchOpen] = useState(false)

  // Heartbeats land on the directory every ~15s; poll at the same cadence so
  // a device that goes quiet fades without a manual reload. SSE covers the
  // structural changes (a node joining, a task moving).
  useVisibleInterval(() => setTick((v) => v + 1), 15000)

  // The project directory table, refreshed on the change feed: a send that
  // mints a project (a directory nothing pointed at yet) has to show up here
  // before the picker can list what it now owns.
  const { data: projects } = useAsync(() => api.projects(), [], tick)

  // Seed the bench with the active project's directory, the way the thread
  // composer's footer does: the pick is a statement first and a control
  // second, and an empty footer would make the history picker useless on
  // arrival. A directory the user chose always wins over the seed.
  const seeded = useRef(false)
  useEffect(() => {
    if (seeded.current || dirTouched || !projects) return
    seeded.current = true
    const active = (projects.detail ?? []).find((d) => d.name === projects.active)
    if (active?.work_dir) setPickedDir(active.work_dir)
  }, [projects, dirTouched])

  const project = useMemo(() => projectOfDir(projects?.detail, pickedDir), [projects, pickedDir])

  // The directory's threads, newest first. Scoped by project because a
  // session's work dir is read from its project row — the picker can only
  // offer what a send can actually continue. It follows the page's own 15s
  // tick rather than the task feed: reading every session file on each task
  // event would be a lot of I/O for a list that changes when a thread does,
  // not when a task does.
  const { data: dirSessions } = useAsync(
    () => (project ? api.sessions(project) : Promise.resolve([] as Session[])),
    [project],
    tick,
  )

  useEffect(() => {
    const el = wrap.current
    if (!el) return
    const ro = new ResizeObserver(() => {
      const r = el.getBoundingClientRect()
      // The drawing is sized to the box it is given (the pane fills the
      // window), so the viewBox tracks the measured height as well — an
      // aspect-corrected viewBox would letterbox the canvas instead.
      if (r.width > 0 && r.height > 0) setSize({ w: r.width, h: r.height })
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const devices = useMemo(() => buildDevices(nodes ?? [], self), [nodes, self])
  const edges = useMemo(() => buildEdges(devices), [devices])
  const { depth } = useMemo(() => ringDepths(devices, edges), [devices, edges])
  const placed = useMemo(() => layout(devices, depth, size.w, size.h), [devices, depth, size.w, size.h])
  const posOf = useMemo(() => new Map(placed.map((p) => [p.device.id, p])), [placed])
  const deviceOf = useMemo(() => new Map(devices.map((d) => [d.id, d])), [devices])

  // Hovering a plate lights its own lines; hovering a line's endpoints lights
  // the line. Either way everything unrelated fades, which is what makes a
  // five-device mesh readable at a glance.
  const litEdges = useMemo(() => {
    const set = new Set<number>()
    if (!hover) return set
    edges.forEach((e, i) => {
      if (e.a === hover || e.b === hover) set.add(i)
    })
    return set
  }, [edges, hover])

  const neighborsOf = useMemo(() => {
    const m = new Map<string, Set<string>>()
    const add = (a: string, b: string) => {
      const set = m.get(a) ?? new Set<string>()
      set.add(b)
      m.set(a, set)
    }
    for (const e of edges) {
      add(e.a, e.b)
      add(e.b, e.a)
    }
    return m
  }, [edges])

  const zoomAt = (cx: number, cy: number, factor: number) => {
    setView((v) => {
      const k = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, v.k * factor))
      const s = k / v.k
      return { k, x: cx - (cx - v.x) * s, y: cy - (cy - v.y) * s }
    })
  }

  // Wheel zoom needs a non-passive listener: a synthetic handler cannot
  // preventDefault, and the page would scroll behind the map.
  useEffect(() => {
    const el = svgRef.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const r = el.getBoundingClientRect()
      const s = size.w / r.width
      zoomAt((e.clientX - r.left) * s, (e.clientY - r.top) * s, e.deltaY < 0 ? 1.12 : 1 / 1.12)
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [size.w])

  const sel = selected ? deviceOf.get(selected) ?? null : null
  // The two cards hang off the same corner and reserve the same lane, so only
  // one can be up at a time: stacking them would paint the orchestration card
  // over the device list, and the composer — which shifts by the lane — would
  // land somewhere different depending on which one was clicked last.
  const toggleRail = () => {
    const next = !railOpen
    setRailOpen(next)
    if (next) setOrchOpen(false)
  }
  const toggleOrch = (open: boolean) => {
    setOrchOpen(open)
    if (open) setRailOpen(false)
  }
  // Picking a device is a request for its facts, and those only live in the
  // rail — so a pick opens the panel it writes to. Clearing a selection
  // leaves the panel's own state alone.
  const pick = (id: string | null) => {
    setSelected(id)
    if (id) toggleOrch(false)
    if (id) setRailOpen(true)
  }
  // The card's own ×: a close button on a panel puts the panel away, it does
  // not leave a list standing in place of the facts that were being read.
  const closeRail = () => {
    setSelected(null)
    setRailOpen(false)
  }

  // Escape closes the card, like the two cards on a thread: the trigger and
  // the key are the only two ways out, so a click on the map never puts the
  // list away mid-read.
  useEffect(() => {
    if (!railOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || layerAboveOwnsEscape()) return
      e.stopPropagation()
      setRailOpen(false)
    }
    document.addEventListener('keydown', onKey, true)
    return () => document.removeEventListener('keydown', onKey, true)
  }, [railOpen])

  if (error)
    return (
      <ErrorState
        title={t('nav.workspace')}
        sub={t('workspace.subtitle')}
        error={error}
        onRetry={() => setTick((v) => v + 1)}
      />
    )

  return (
    <section class={`ws-page${orchOpen || railOpen ? ' panel-open' : ''}`}>
      {/* Same top edge as a thread: a mark, the name, the key for the
          drawing, and the two controls the page has. The drawing below is the
          page — no counts, no toolbar. The orchestration card is the exception:
          this surface never shows a transcript, so the tasks a directory has
          produced are the only place a dispatched turn becomes visible. It sits
          at the far right, exactly where the chat's own card hangs. */}
      <header class="ws-head">
        <Icon name="devices" size={16} class="ws-head-icon" />
        <span class="ws-title">{t('nav.workspace')}</span>
        {/* The key reads as a subtitle rather than a footer: one line of
            glyph + name for the six machine classes the plates encode. The
            two line styles are still named by the edges' own tooltips. */}
        <div class="ws-legend">
          {FORMS.map((f) => (
            <span class="ws-legend-item" key={f}>
              <svg viewBox="-14 -14 28 28" aria-hidden="true">
                <DeviceGlyph form={f} />
              </svg>
              {formLabel(f)}
            </span>
          ))}
        </div>
        <div class="ws-head-tools">
          {/* The rail is a card, not a column: it hangs off the same corner as
              the orchestration card beside it, over the drawing. Taking a grid
              column instead made the map narrower, which re-laid the plates out
              under the reader's cursor every time the list opened — the fleet
              moved because a list appeared. */}
          <div class="ws-rail-picker">
            <button
              type="button"
              class={`icon-btn ws-rail-toggle${railOpen ? ' on' : ''}`}
              aria-label={t('workspace.rail')}
              aria-expanded={railOpen}
              title={t('workspace.rail')}
              onClick={toggleRail}
            >
              <Icon name="panel-right" size={16} />
            </button>
            {railOpen && (
              <aside class="ws-rail" role="dialog" aria-label={t('workspace.rail')}>
                {sel && (
                  <DeviceDetail
                    device={sel}
                    devices={deviceOf}
                    onSelect={(id) => (id ? pick(id) : closeRail())}
                  />
                )}
                <div class="ws-list">
                  {devices.map((d) => (
                    <button
                      type="button"
                      key={d.id}
                      class={`ws-row${selected === d.id ? ' sel' : ''}${d.online ? '' : ' off'}`}
                      onClick={() => pick(selected === d.id ? null : d.id)}
                      onMouseEnter={() => setHover(d.id)}
                      onMouseLeave={() => setHover((v) => (v === d.id ? null : v))}
                    >
                      <svg class="ws-row-glyph" viewBox="-14 -14 28 28" aria-hidden="true">
                        <DeviceGlyph form={d.form} />
                      </svg>
                      <span class="ws-row-text">
                        <span class="ws-row-name">{d.name}</span>
                        <span class="ws-row-meta dim">
                          {osLabel(d.os)} · {formLabel(d.form)}
                        </span>
                      </span>
                      <span class={`ws-row-dot${d.online ? '' : ' off'}`} aria-hidden="true" />
                      <span class="ws-row-rtt mono dim">{d.rtt > 0 ? `${d.rtt} ms` : ''}</span>
                    </button>
                  ))}
                </div>
              </aside>
            )}
          </div>
          <OrchestrateCard
            key={`orch-${project || 'none'}`}
            project={project || undefined}
            open={orchOpen}
            onOpenChange={toggleOrch}
            onlineNodeCount={devices.filter((d) => d.online).length}
            selfNodeId={devices.find((d) => d.local)?.id}
          />
        </div>
      </header>

      <div class="ws-body">
        <div class="ws-main">
          <div class={`ws-map${panning ? ' panning' : ''}`} ref={wrap}>
            {devices.length === 0 ? (
              <p class="dim ws-empty">{t('workspace.empty')}</p>
            ) : (
              <svg
                ref={svgRef}
                class="ws-canvas"
                viewBox={`0 0 ${size.w} ${size.h}`}
                role="img"
                aria-label={t('nav.workspace')}
                onDblClick={(e) => {
                  // Double-click on empty canvas recentres the drawing — the
                  // page has no reset button any more, and a wheel zoom with
                  // no way home is a trap. A double-click on a plate is a
                  // double pick, not a reset.
                  if ((e.target as Element).classList?.contains('ws-bg')) setView(VIEW_HOME)
                }}
                onPointerDown={(e) => {
                  const el = e.target as Element
                  if (!el.classList?.contains('ws-bg')) return
                  el.setPointerCapture?.(e.pointerId)
                  drag.current = { px: e.clientX, py: e.clientY, vx: view.x, vy: view.y }
                  setPanning(true)
                }}
                onPointerMove={(e) => {
                  const d = drag.current
                  if (!d) return
                  const r = svgRef.current!.getBoundingClientRect()
                  const s = size.w / r.width
                  setView((v) => ({
                    ...v,
                    x: d.vx + (e.clientX - d.px) * s,
                    y: d.vy + (e.clientY - d.py) * s,
                  }))
                }}
                onPointerUp={() => {
                  drag.current = null
                  setPanning(false)
                }}
                onPointerCancel={() => {
                  drag.current = null
                  setPanning(false)
                }}
              >
                <defs>
                  <pattern id="ws-dots" width="26" height="26" patternUnits="userSpaceOnUse">
                    <circle cx="1.1" cy="1.1" r="1.1" style={{ fill: 'var(--border-soft)' }} />
                  </pattern>
                  <filter id="ws-plate-shadow" x="-60%" y="-60%" width="220%" height="220%">
                    <feDropShadow dx="0" dy="1.4" stdDeviation="2.2" flood-opacity="0.16" />
                  </filter>
                  <filter id="ws-edge-glow" x="-40%" y="-40%" width="180%" height="180%">
                    <feDropShadow
                      dx="0"
                      dy="0"
                      stdDeviation="2.2"
                      style={{ floodColor: 'var(--accent)', floodOpacity: 0.5 }}
                    />
                  </filter>
                </defs>
                <rect width={size.w} height={size.h} fill="url(#ws-dots)" opacity="0.5" />
                <g transform={`translate(${view.x} ${view.y}) scale(${view.k})`}>
                  <rect
                    class="ws-bg"
                    x={-size.w * 3}
                    y={-size.h * 3}
                    width={size.w * 7}
                    height={size.h * 7}
                  />
                  {edges.map((e, i) => {
                    const a = posOf.get(e.a)
                    const b = posOf.get(e.b)
                    if (!a || !b) return null
                    const dx = b.x - a.x
                    const dy = b.y - a.y
                    const dist = Math.max(Math.hypot(dx, dy), 1)
                    const ux = dx / dist
                    const uy = dy / dist
                    // Stop at the plate rim so the line meets the device rather
                    // than disappearing under it.
                    const gap = PLATE / 2 + 5
                    const x1 = a.x + ux * gap
                    const y1 = a.y + uy * gap
                    const x2 = b.x - ux * gap
                    const y2 = b.y - uy * gap
                    const lit = litEdges.has(i)
                    const dim = hover !== null && !lit
                    return (
                      <g key={`${e.a}-${e.b}`} class={`ws-edge-g${dim ? ' dim' : ''}${lit ? ' lit' : ''}`}>
                        <line
                          class={`ws-edge ws-edge-${e.kind}`}
                          x1={x1}
                          y1={y1}
                          x2={x2}
                          y2={y2}
                        />
                        <line class="ws-edge-hit" x1={x1} y1={y1} x2={x2} y2={y2}>
                          <title>
                            {e.kind === 'mesh' ? t('workspace.edge.mesh') : t('workspace.edge.measured')}
                            {e.rtt > 0 ? ` · ${e.rtt} ms` : ''}
                          </title>
                        </line>
                      </g>
                    )
                  })}
                  {placed.map(({ device: d, x, y }) => {
                    const nb = neighborsOf.get(d.id)
                    const dim = hover !== null && hover !== d.id && !(nb?.has(hover) ?? false)
                    return (
                      <g
                        key={d.id}
                        class={`ws-node${d.online ? '' : ' off'}${d.local ? ' self' : ''}${
                          selected === d.id ? ' sel' : ''
                        }${dim ? ' dim' : ''}`}
                        transform={`translate(${x},${y})`}
                        role="button"
                        tabIndex={0}
                        aria-label={`${d.name} · ${formLabel(d.form)} · ${osLabel(d.os)}`}
                        aria-pressed={selected === d.id}
                        onMouseEnter={() => setHover(d.id)}
                        onMouseLeave={() => setHover((v) => (v === d.id ? null : v))}
                        onFocus={() => setHover(d.id)}
                        onBlur={() => setHover((v) => (v === d.id ? null : v))}
                        onClick={() => pick(selected === d.id ? null : d.id)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault()
                            pick(selected === d.id ? null : d.id)
                          } else if (e.key === 'Escape') {
                            setSelected(null)
                          }
                        }}
                      >
                        {selected === d.id && <circle class="ws-halo" r={47} />}
                        <rect
                          class="ws-plate"
                          x={-31}
                          y={-31}
                          width={62}
                          height={62}
                          rx={16}
                          filter="url(#ws-plate-shadow)"
                        />
                        <g class="ws-glyph" transform="scale(1.5)">
                          <DeviceGlyph form={d.form} />
                        </g>
                        <circle class="ws-status" cx={23} cy={-23} r={3.4} />
                        {d.local && (
                          <text class="ws-tag" y={27} text-anchor="middle">
                            {t('workspace.local')}
                          </text>
                        )}
                        <text class="ws-name" y={46} text-anchor="middle">
                          {d.name.length > 16 ? `${d.name.slice(0, 15)}…` : d.name}
                        </text>
                        <text class="ws-os" y={59} text-anchor="middle">
                          {osLabel(d.os)} · {formLabel(d.form)}
                        </text>
                      </g>
                    )
                  })}
                </g>
              </svg>
            )}
          </div>
        </div>
      </div>

      {/* The chat composer, reused whole and docked exactly where 新对话 has
          it: same box, same type, same Enter rules, same footer. The footer
          now carries both halves of the bench — the directory, and the thread
          of that directory the next task is appended to — and a send stays
          here: no thread opens, no transcript is painted, the orchestration
          card is where the dispatched work becomes visible. */}
      <WorkspaceComposer
        dir={pickedDir}
        onDirChange={(path) => {
          setPickedDir(path)
          setDirTouched(true)
          // A thread belongs to the directory it was listed under; keeping it
          // picked across a directory switch would silently append the next
          // task to a conversation from somewhere else.
          setPickedSession(null)
        }}
        project={project}
        sessions={dirSessions ?? []}
        picked={pickedSession}
        onPick={setPickedSession}
        onSent={(s) => {
          // A new thread becomes the pick: the next send continues what this
          // one started, the same rule the chat composer follows.
          setPickedSession(s)
          setTick((v) => v + 1)
          // Feedback, since there is no transcript to watch: the card that
          // answers "what did I just dispatch" is the task list.
          toggleOrch(true)
        }}
      />
    </section>
  )
}

/** Resolve a picked directory to the project whose work dir it is, creating
 *  the project when no row points at it yet — the same rule the thread
 *  composer's footer follows. */
async function projectForDir(dir: string): Promise<string> {
  const pl = await api.projects()
  const norm = (p: string) => p.replace(/[\\/]+$/, '').toLowerCase()
  const want = norm(dir)
  const hit = (pl.detail ?? []).find((d) => d.work_dir && norm(d.work_dir) === want)
  if (hit) return hit.name
  const taken = new Set(pl.projects ?? [])
  const base = suggestProjectName(dir) || 'project'
  let name = base
  for (let i = 2; taken.has(name); i++) name = `${base}-${i}`
  const created = await api.createProject({ name, work_dir: dir })
  return created.name
}

/** The read-only half of the rule above: which project already owns this
 *  directory, or '' when none does. The picker needs the answer without the
 *  side effect — opening 工作空间 on a directory that was never used must not
 *  mint a project just to say "no history here yet". */
function projectOfDir(
  detail: Array<{ name: string; work_dir?: string }> | undefined,
  dir: string,
): string {
  if (!dir) return ''
  const norm = (p: string) => p.replace(/[\\/]+$/, '').toLowerCase()
  const want = norm(dir)
  return (detail ?? []).find((d) => d.work_dir && norm(d.work_dir) === want)?.name ?? ''
}

/** The chat composer, reused whole: same box, same type, same Enter rules,
 *  same footer. What a send *does* is the difference — it is a dispatch, not
 *  a turn in the page you are looking at:
 *
 *  - the target thread is the footer's pick, or a brand-new one when nothing
 *    is picked (titled after the prompt, like 新对话);
 *  - the stream is fired and never awaited. There is no transcript here, so
 *    there is nothing to paint: the turn runs on the server and the header's
 *    task card is where its progress shows up;
 *  - nothing navigates. Not on send, not after it — staying on the map is the
 *    whole point of the surface, and leaving the page must not cancel work. */
function WorkspaceComposer(props: {
  dir: string
  onDirChange(path: string): void
  /** The project the picked directory resolves to; '' when nothing owns it. */
  project: string
  sessions: Session[]
  picked: Session | null
  onPick(session: Session | null): void
  /** Called with the thread the task was sent to, after the stream is up. */
  onSent(session: Session): void
}) {
  useLocaleRerender()
  const composer = useRef<HTMLDivElement>(null)
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [authorize, setAuthorize] = useState(false)

  useEffect(() => {
    composer.current?.focus()
    // No abort on unmount on purpose: this is a dispatch surface, and a turn
    // that survives the page being left is the feature, not a leak.
  }, [])

  async function send(e?: Event) {
    e?.preventDefault()
    const raw = input.trim()
    if (!raw || sending) return
    setSending(true)
    try {
      // The directory resolves first: a session's work dir is read from its
      // project row, so a directory no project points at would be silently
      // ignored. An existing project is reused; a new directory gets one
      // minted so the thread — and its tasks — are filed where the picker can
      // find them next time.
      let threadProject: string | undefined = props.project || undefined
      if (!threadProject && props.dir) {
        try {
          threadProject = await projectForDir(props.dir)
        } catch (err) {
          // The send matters more than the pick — report and fall back to the
          // default project, the same trade the thread composer makes.
          toastError(err)
        }
      }
      // @file references expand to fenced blocks before the prompt leaves —
      // the same rule the thread composer applies.
      const expanded = await expandFileRefs(raw, (p) => api.fsRead(p))
      const prompt = expanded.prompt
      const target =
        props.picked ?? (await api.createSession(prompt.slice(0, 48), threadProject))
      void askSessionStream(
        target.id,
        prompt,
        authorize,
        {
          onDelta() {},
          onReasoning() {},
          onStatus() {},
          onOperation() {},
          onResult() {},
          // The one handler that has to do something: with no transcript, a
          // turn that failed (no model, dead key, rejected prompt) would
          // otherwise vanish without a trace.
          onError(msg) {
            toast(msg, 'error')
          },
        },
        undefined,
      ).catch(toastError)
      editorClear(composer.current)
      setInput('')
      props.onSent(target)
      toast(
        t('workspace.sentToast', { title: target.title || t('sessions.untitled') }),
        'success',
      )
    } catch (err) {
      toastError(err)
    } finally {
      setSending(false)
    }
  }

  return (
    <form class="composer" onSubmit={send}>
      <div class="composer-box">
        <div
          ref={(el) => {
            composer.current = el
          }}
          class={`composer-input${editorPlain(input) === '' ? ' is-empty' : ''}`}
          contenteditable="true"
          role="textbox"
          aria-multiline="true"
          aria-label={t('sessions.placeholder')}
          data-placeholder={t('sessions.placeholder')}
          onInput={(e) => {
            useBrParagraphSeparator()
            const el = e.currentTarget as HTMLDivElement
            setInput(editorTextOf(el))
            autoGrow(el)
          }}
          onKeyDown={(e) => {
            // Shift+Enter breaks the line — the browser's own newline is a
            // literal text node Chromium does not lay out here, so the <br> is
            // inserted by hand, exactly as the thread composer does.
            if (e.key === 'Enter' && e.shiftKey && !e.isComposing) {
              e.preventDefault()
              const el = e.currentTarget as HTMLDivElement
              const br = el.ownerDocument.createElement('br')
              const sel = document.getSelection()
              if (sel && sel.rangeCount > 0) {
                const rg = sel.getRangeAt(0)
                rg.deleteContents()
                rg.insertNode(br)
                rg.setStartAfter(br)
                rg.collapse(true)
                sel.removeAllRanges()
                sel.addRange(rg)
              } else {
                el.appendChild(br)
              }
              range_or_append(br, el.ownerDocument.createTextNode('\u200b'))
              setInput(editorTextOf(el))
              autoGrow(el)
              return
            }
            // Enter sends; a composing IME keeps its Enter.
            if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && !(e.metaKey || e.ctrlKey)) {
              e.preventDefault()
              void send()
            }
            if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
              e.preventDefault()
              void send()
            }
          }}
        />
        <div class="composer-actions">
          <label class="ask-authorize" title={t('approval.stateHint')}>
            <span class="switch">
              <input
                type="checkbox"
                checked={authorize}
                onChange={(e) => setAuthorize((e.target as HTMLInputElement).checked)}
              />
              <span class="switch-track" aria-hidden="true" />
            </span>
            {t('sessions.authorize')}
          </label>
          <ModelSelect disabled={sending} />
          <button class="btn primary" type="submit" disabled={!input.trim() || sending}>
            {t('sessions.send')}
          </button>
        </div>
      </div>
      {/* Both halves of the bench, in the footer's own grammar: where the task
          runs, then which thread it continues. The thread pill is the reason
          this page exists — a send appends to the picked conversation without
          ever opening it. */}
      <div class="composer-foot">
        <DirButton
          value={props.dir}
          onChange={props.onDirChange}
          label={t('sessions.chooseDir')}
          disabled={sending}
        />
        <SessionPick
          sessions={props.sessions}
          value={props.picked}
          onChange={props.onPick}
          hasDir={props.dir !== ''}
          disabled={sending}
        />
        <p class="composer-shortcut-hint">{t('sessions.sendShortcut')}</p>
      </div>
    </form>
  )
}

/** One device, in full: the facts the map cannot show (chip, memory, load,
 *  trust, version) plus its live adjacency, each neighbor a jump target. */
function DeviceDetail({
  device: d,
  devices,
  onSelect,
}: {
  device: Device
  devices: Map<string, Device>
  onSelect(id: string | null): void
}) {
  const neighbors = d.neighbors.filter((id) => devices.has(id))
  return (
    <div class="ws-detail">
      <div class="ws-detail-head">
        <svg class="ws-detail-glyph" viewBox="-14 -14 28 28" aria-hidden="true">
          <DeviceGlyph form={d.form} />
        </svg>
        <span class="ws-detail-name">{d.name}</span>
        <button
          type="button"
          class="icon-btn"
          aria-label={t('common.close')}
          onClick={() => onSelect(null)}
        >
          ×
        </button>
      </div>
      <p class="ws-detail-id mono dim">{d.id}</p>
      <div class="ws-detail-badges">
        <span class={`badge ${d.online ? 'green' : ''}`}>
          {d.online ? t('workspace.online') : t('workspace.offline')}
        </span>
        {d.local && <span class="badge green">{t('workspace.local')}</span>}
        <span class="badge">{formLabel(d.form)}</span>
        <span class="badge">{osLabel(d.os)}</span>
        {d.trust && d.trust !== 'mesh' && <span class="badge">{d.trust}</span>}
      </div>
      {d.chip && (
        <div class="ws-detail-row">
          <span class="dim">{t('workspace.detail.chip')}</span>
          <span class="mono ws-detail-val">{d.chip}</span>
        </div>
      )}
      <div class="ws-detail-row">
        <span class="dim">{t('workspace.detail.resources')}</span>
        <span class="mono">
          {t('nodes.cores', { n: d.cores })}
          {d.ram > 0 ? ` · ${d.ram} GB` : ''}
        </span>
      </div>
      {d.maxTasks > 0 && (
        <div class="ws-detail-row">
          <span class="dim">{t('workspace.detail.tasks')}</span>
          <span class="mono">{t('nodes.concurrency', { cur: d.tasks, max: d.maxTasks })}</span>
        </div>
      )}
      {d.rtt > 0 && (
        <div class="ws-detail-row">
          <span class="dim">{t('workspace.detail.rtt')}</span>
          <span class="mono">
            {d.rtt} ms{d.transport ? ` · ${d.transport}` : ''}
          </span>
        </div>
      )}
      <div class="ws-detail-row">
        <span class="dim">{t('nodes.lastSeen')}</span>
        <span class="mono">
          {d.lastSeen === 'never' ? t('nodes.never') : new Date(d.lastSeen).toLocaleString()}
        </span>
      </div>
      {d.ver && (
        <div class="ws-detail-row">
          <span class="dim">{t('workspace.detail.version')}</span>
          <span class="mono">{d.ver}</span>
        </div>
      )}
      <div class="ws-detail-links">
        <span class="dim">{t('workspace.detail.links')}</span>
        {neighbors.length === 0 ? (
          <p class="dim ws-detail-none">{t('workspace.detail.none')}</p>
        ) : (
          <div class="ws-detail-chips">
            {neighbors.map((id) => (
              <button type="button" class="ws-chip" key={id} onClick={() => onSelect(id)}>
                {devices.get(id)?.name ?? id}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
