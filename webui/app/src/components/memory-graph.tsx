// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import type { MemoryGraph, MemoryGraphEdge, MemoryGraphNode } from '../api/client'
import { t } from '../i18n'
import {
  ENTITY_KIND,
  filterGraph,
  fitText,
  isEntity,
  labelLines,
  layoutGraph,
  nodeExtent,
  nodeRadius,
  recencyAlpha,
  seenDates,
  type LayerMode,
} from './graph-layout'

/** The memory knowledge graph: two layers over the same evidence. Provenance
 *  ("file") nodes are the memory surfaces and skills; knowledge ("entity")
 *  nodes are terms literally recurring inside them. Edges are promotions,
 *  name references, entity mentions and co-occurrence — every one of them a
 *  string the files actually contain. Rendering is dependency-free SVG over
 *  the graph-layout module: deterministic clustering, no-overlap relaxation,
 *  wheel zoom, drag-to-pan, a first-seen timeline and layer/kind filters. */

interface View {
  k: number // zoom scale
  x: number // pan offset, graph units
  y: number
}

const VIEW_HOME: View = { k: 1, x: 0, y: 0 }
const ZOOM_MIN = 0.4
const ZOOM_MAX = 5

const KIND_CLASS: Record<string, string> = {
  core: 'mg-core',
  topic: 'mg-topic',
  project: 'mg-project',
  daily: 'mg-daily',
  skill: 'mg-skill',
  [ENTITY_KIND]: 'mg-entity',
}

/** Kind → (strong, soft) theme vars feeding the node's radial gradient — a
 *  pale core with a tinted rim reads as a sphere instead of a flat disc. */
const KIND_FILL: Record<string, [strong: string, soft: string]> = {
  core: ['accent', 'accent-soft'],
  topic: ['accent-2', 'accent-2-soft'],
  project: ['semantic-info', 'semantic-info-soft'],
  daily: ['semantic-warn', 'semantic-warn-soft'],
  skill: ['danger', 'danger-soft'],
  [ENTITY_KIND]: ['semantic-good', 'semantic-good-soft'],
}

const EDGE_CLASS: Record<string, string> = {
  promotion: 'mg-edge-promotion',
  reference: 'mg-edge-reference',
  mention: 'mg-edge-mention',
  co: 'mg-edge-co',
}

const FILE_KIND_ORDER = ['core', 'topic', 'project', 'daily', 'skill'] as const

export function MemoryGraphView({
  graph,
  onOpen,
}: {
  graph: MemoryGraph
  onOpen?(node: MemoryGraphNode): void
}) {
  const wrap = useRef<HTMLDivElement>(null)
  const svgRef = useRef<SVGSVGElement>(null)
  const [size, setSize] = useState({ w: 860, h: 560 })
  const [hover, setHover] = useState<string | null>(null)
  const [selected, setSelected] = useState<string | null>(null)
  const [view, setView] = useState<View>(VIEW_HOME)
  const [panning, setPanning] = useState(false)
  const [layer, setLayer] = useState<LayerMode>('all')
  const [hiddenKinds, setHiddenKinds] = useState<ReadonlySet<string>>(new Set())
  const [until, setUntil] = useState('')
  const [playing, setPlaying] = useState(false)
  const drag = useRef<{ px: number; py: number; vx: number; vy: number } | null>(null)

  useEffect(() => {
    const el = wrap.current
    if (!el) return
    const ro = new ResizeObserver(() => {
      const r = el.getBoundingClientRect()
      if (r.width > 0) setSize({ w: r.width, h: Math.max(460, Math.min(720, r.width * 0.66)) })
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const zoomAt = (cx: number, cy: number, factor: number) => {
    setView((v) => {
      const k = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, v.k * factor))
      const s = k / v.k
      return { k, x: cx - (cx - v.x) * s, y: cy - (cy - v.y) * s }
    })
  }

  // Manual wheel binding: React-style synthetic handlers cannot preventDefault
  // inside a passive listener, so the zoom lives on a real non-passive one.
  useEffect(() => {
    const el = svgRef.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const r = el.getBoundingClientRect()
      const s = size.w / r.width
      zoomAt((e.clientX - r.left) * s, (e.clientY - r.top) * s, e.deltaY < 0 ? 1.15 : 1 / 1.15)
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [size.w])

  // Timeline: play walks the distinct first-seen dates; nodes without one
  // (files that predate the diary) stay visible throughout.
  const steps = useMemo(() => seenDates(graph), [graph])
  const stepIdx = until ? Math.max(0, steps.indexOf(until)) : steps.length - 1
  const effectiveUntil = until || steps[steps.length - 1] || ''
  useEffect(() => {
    if (!playing) return
    let i = stepIdx
    const id = window.setInterval(() => {
      i += 1
      const step = steps[i]
      if (!step) {
        setPlaying(false)
        return
      }
      setUntil(step)
    }, 460)
    return () => window.clearInterval(id)
    // stepIdx is read once per playback run on purpose: the interval advances
    // from wherever the user left the slider.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [playing, steps])

  const filtered = useMemo(
    () => filterGraph(graph, { layer, hiddenKinds, until: effectiveUntil }),
    [graph, layer, hiddenKinds, effectiveUntil],
  )
  // Positions persist across filter/timeline changes: the layout is seeded
  // with the previous one (same canvas size), so playing the timeline makes
  // nodes appear in place rather than reshuffling the whole picture, and the
  // relax loop's early exit makes each tick nearly free.
  const prevLayout = useRef<{ w: number; h: number; pos: Map<string, { x: number; y: number }> } | null>(null)
  const layout = useMemo(() => {
    const prev = prevLayout.current
    const seed = prev && prev.w === size.w && prev.h === size.h ? prev.pos : undefined
    const next = layoutGraph(filtered, size.w, size.h, seed)
    prevLayout.current = {
      w: size.w,
      h: size.h,
      pos: new Map([...next.pos].map(([id, p]) => [id, { x: p.x, y: p.y }])),
    }
    return next
  }, [filtered, size])
  const pos = layout.pos

  const byId = useMemo(() => new Map(filtered.nodes.map((n) => [n.id, n])), [filtered])

  // Parallel edges between the same pair fan out: each gets an alternating,
  // growing perpendicular offset so a promotion and a reference between the
  // same surfaces never draw on top of each other.
  const edgeSpread = useMemo(() => {
    const seen = new Map<string, number>()
    const out = new Map<number, number>()
    filtered.edges.forEach((e, i) => {
      const pair = [e.from, e.to].sort().join('|')
      const n = seen.get(pair) ?? 0
      seen.set(pair, n + 1)
      out.set(i, (n % 2 === 0 ? 1 : -1) * (16 + Math.floor(n / 2) * 26))
    })
    return out
  }, [filtered])

  const neighbors = useMemo(() => {
    const set = new Set<string>()
    if (hover) {
      for (const e of filtered.edges) {
        if (e.from === hover) set.add(e.to)
        if (e.to === hover) set.add(e.from)
      }
    }
    return set
  }, [filtered, hover])

  const sel = selected ? byId.get(selected) : undefined
  const selEdges = useMemo(
    () => (sel ? filtered.edges.filter((e) => e.from === sel.id || e.to === sel.id) : []),
    [filtered, sel],
  )
  const labelOf = (id: string) => byId.get(id)?.label ?? id

  const kindsPresent = useMemo(() => {
    const set = new Set(filtered.nodes.map((n) => n.kind))
    return set
  }, [filtered])

  const chips = useMemo(() => {
    const order = layer === 'entities' ? [ENTITY_KIND] : layer === 'files' ? [...FILE_KIND_ORDER] : [...FILE_KIND_ORDER, ENTITY_KIND]
    return order.filter((k) => kindsPresent.has(k))
  }, [layer, kindsPresent])

  const hasEntities = useMemo(() => graph.nodes.some(isEntity), [graph])
  const hasFiles = useMemo(() => graph.nodes.some((n) => !isEntity(n)), [graph])
  const edgeLegend = hasEntities
    ? (['promotion', 'reference', 'mention', 'co'] as const)
    : (['promotion', 'reference'] as const)

  const now = Date.now()
  const viewDirty = Math.abs(view.k - 1) > 0.01 || Math.abs(view.x) > 1 || Math.abs(view.y) > 1

  const toggleKind = (kind: string) => {
    setHiddenKinds((prev) => {
      const next = new Set(prev)
      if (next.has(kind)) next.delete(kind)
      else next.add(kind)
      return next
    })
  }

  return (
    <div class={`mg-wrap${panning ? ' panning' : ''}`} ref={wrap}>
      <div class="mg-toolbar">
        <div class="segmented mg-layers" role="group" aria-label={t('memory.graph.layers')}>
          {(['all', 'files', 'entities'] as const).map((m) => {
            const empty = (m === 'files' && !hasFiles) || (m === 'entities' && !hasEntities)
            return (
              <button
                key={m}
                type="button"
                class={`seg${layer === m ? ' on' : ''}`}
                aria-pressed={layer === m}
                disabled={empty}
                onClick={() => setLayer(m)}
              >
                {t(`memory.graph.layer.${m}`)}
              </button>
            )
          })}
        </div>
        <span class="mg-stats dim mono">
          {t('memory.graph.nodesN', { n: filtered.nodes.length })} ·{' '}
          {t('memory.graph.edgesN', { n: filtered.edges.length })}
        </span>
        <div class="mg-toolbar-right">
          <span class="dim mg-hint-t">{t('memory.graph.hint')}</span>
          <button type="button" class="btn small" aria-label={t('memory.graph.zoomOut')} onClick={() => zoomAt(size.w / 2, size.h / 2, 1 / 1.3)}>
            −
          </button>
          <button type="button" class="btn small" aria-label={t('memory.graph.zoomIn')} onClick={() => zoomAt(size.w / 2, size.h / 2, 1.3)}>
            +
          </button>
          {viewDirty && (
            <button type="button" class="btn small" onClick={() => setView(VIEW_HOME)}>
              {t('memory.graph.reset')}
            </button>
          )}
        </div>
      </div>
      {filtered.nodes.length === 0 ? (
        <p class="dim mg-empty">
          {t(graph.nodes.length === 0 ? 'memory.graphEmpty' : 'memory.graph.filteredEmpty')}
        </p>
      ) : (
        <svg
          ref={svgRef}
          class="mg-canvas"
          viewBox={`0 0 ${size.w} ${size.h}`}
          role="img"
          aria-label={t('memory.graphTitle')}
          onPointerDown={(e) => {
            const el = e.target as Element
            if (!el.classList?.contains('mg-bg')) return
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
            {Object.entries(KIND_FILL).map(([kind, [strong, soft]]) => (
              <radialGradient key={kind} id={`mg-fill-${kind}`} cx="38%" cy="30%" r="95%">
                <stop offset="0%" style={{ stopColor: `var(--${soft})` }} />
                <stop offset="100%" style={{ stopColor: `var(--${strong})`, stopOpacity: 0.3 }} />
              </radialGradient>
            ))}
            {/* Viewport-fixed dot grid — the canvas texture, deliberately not
                inside the zoom group so panning slides content over it. */}
            <pattern id="mg-dots" width="26" height="26" patternUnits="userSpaceOnUse">
              <circle cx="1.1" cy="1.1" r="1.1" style={{ fill: 'var(--border-soft)' }} />
            </pattern>
            <filter id="mg-node-shadow" x="-60%" y="-60%" width="220%" height="220%">
              <feDropShadow dx="0" dy="1.6" stdDeviation="2.4" flood-opacity="0.28" />
            </filter>
            <filter id="mg-edge-glow" x="-30%" y="-30%" width="160%" height="160%">
              <feDropShadow dx="0" dy="0" stdDeviation="2.4" style={{ floodColor: 'var(--accent)', floodOpacity: 0.65 }} />
            </filter>
            <marker
              id="mg-arr-ref"
              viewBox="0 0 10 10"
              refX="8.5"
              refY="5"
              markerWidth="9"
              markerHeight="9"
              markerUnits="userSpaceOnUse"
              orient="auto-start-reverse"
            >
              <path d="M 0.5 1 L 9 5 L 0.5 9 z" class="mg-arrow-ref" />
            </marker>
            <marker
              id="mg-arr-promo"
              viewBox="0 0 10 10"
              refX="8.5"
              refY="5"
              markerWidth="9"
              markerHeight="9"
              markerUnits="userSpaceOnUse"
              orient="auto-start-reverse"
            >
              <path d="M 0.5 1 L 9 5 L 0.5 9 z" class="mg-arrow-promo" />
            </marker>
          </defs>
          <rect width={size.w} height={size.h} fill="url(#mg-dots)" opacity="0.55" />
          <g transform={`translate(${view.x} ${view.y}) scale(${view.k})`}>
            <rect class="mg-bg" x={-size.w * 4} y={-size.h * 4} width={size.w * 9} height={size.h * 9} />
            {/* Cluster homes: a soft tint behind each kind's anchor so the
                picture reads as grouped knowledge, not loose dots. */}
            {layout.clusters.map((c) => (
              <g key={c.key} class={`mg-cluster ${KIND_CLASS[c.key] ?? 'mg-core'}`}>
                <circle cx={c.x} cy={c.y} r={c.r} />
                <text x={c.x} y={c.y - c.r - 8}>
                  {t(`memory.graph.${c.key}`)}
                </text>
              </g>
            ))}
            {filtered.edges.map((e, i) => {
              const a = pos.get(e.from)
              const b = pos.get(e.to)
              if (!a || !b) return null
              const dx = b.x - a.x
              const dy = b.y - a.y
              const dist = Math.max(Math.hypot(dx, dy), 1)
              const ux = dx / dist
              const uy = dy / dist
              // Endpoints pull back to the node rims so the arrowhead lands on
              // the boundary, not buried under the circle or pill.
              const x1 = a.x + ux * (nodeExtent(a) + 3)
              const y1 = a.y + uy * (nodeExtent(a) + 3)
              const x2 = b.x - ux * (nodeExtent(b) + 8)
              const y2 = b.y - uy * (nodeExtent(b) + 8)
              const off = edgeSpread.get(i) ?? 16
              const mx = (x1 + x2) / 2 - uy * off
              const my = (y1 + y2) / 2 + ux * off
              const lit = hover !== null && (e.from === hover || e.to === hover)
              const dim = hover !== null && !lit
              // Quadratic Bézier point at t=0.5: (P0 + 2·C + P2) / 4 — the
              // badge sits on the curve itself, not the control point.
              const lx = (x1 + 2 * mx + x2) / 4
              const ly = (y1 + 2 * my + y2) / 4
              const d = `M ${x1} ${y1} Q ${mx} ${my} ${x2} ${y2}`
              const bw = 9 + String(e.weight ?? 0).length * 4.6
              const arrow = e.kind === 'promotion' ? 'url(#mg-arr-promo)' : e.kind === 'reference' ? 'url(#mg-arr-ref)' : undefined
              return (
                <g
                  key={i}
                  class={`mg-edge-g${dim ? ' dim' : ''}${lit ? ' lit' : ''}`}
                  style={{ animationDelay: `${Math.min(i * 18, 400)}ms` }}
                >
                  <path
                    class={`mg-edge ${EDGE_CLASS[e.kind] ?? 'mg-edge-reference'}`}
                    style={{ strokeWidth: edgeWidth(e) + (lit ? 0.7 : 0) }}
                    d={d}
                    marker-end={arrow}
                  />
                  {/* Fat invisible stroke: a real hit target for the tooltip,
                      the thin visible line alone is hard to hover. */}
                  <path class="mg-edge-hit" d={d}>
                    <title>
                      {t(`memory.graph.${e.kind}`, e.kind)}
                      {(e.weight ?? 0) > 1 ? ` ×${e.weight}` : ''}
                    </title>
                  </path>
                  {(e.weight ?? 0) > 1 && (e.kind === 'promotion' || lit) && (
                    <g class="mg-badge">
                      <rect x={lx - bw / 2} y={ly - 6} width={bw} height={12} rx={6} />
                      <text class="mono" x={lx} y={ly + 3} text-anchor="middle">
                        ×{e.weight}
                      </text>
                    </g>
                  )}
                </g>
              )
            })}
            {filtered.nodes.map((n, i) => {
              const p = pos.get(n.id)!
              const dimmed = hover !== null && hover !== n.id && !neighbors.has(n.id)
              const ent = isEntity(n)
              const ext = nodeExtent(n)
              const r = nodeRadius(n)
              const alpha = recencyAlpha(n.last_seen, now)
              const lines = labelLines(n.label)
              return (
                <g
                  key={n.id}
                  class={`mg-node ${KIND_CLASS[n.kind] ?? 'mg-core'}${dimmed ? ' dim' : ''}${
                    selected === n.id ? ' sel' : ''
                  }${n.status && n.status !== 'active' ? ' pending' : ''}`}
                  transform={`translate(${p.x},${p.y})`}
                  onMouseEnter={() => setHover(n.id)}
                  onMouseLeave={() => setHover((v) => (v === n.id ? null : v))}
                  onFocus={() => setHover(n.id)}
                  onBlur={() => setHover((v) => (v === n.id ? null : v))}
                  onClick={() => setSelected(n.id === selected ? null : n.id)}
                  role="button"
                  tabIndex={0}
                  aria-label={`${t(`memory.graph.${n.kind}`, n.kind)} ${n.label}`}
                  aria-pressed={selected === n.id}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      setSelected(n.id === selected ? null : n.id)
                    } else if (e.key === 'Escape') {
                      setSelected(null)
                    }
                  }}
                >
                  {/* Inner g carries the pop-in — animating transform on the
                      outer one would fight its translate() attribute. */}
                  <g class="mg-pop" style={{ animationDelay: `${Math.min(i * 30, 520)}ms` }}>
                    {ent ? (
                      <>
                        {selected === n.id && (
                          <rect class="mg-halo" x={-ext - 5} y={-16} width={ext * 2 + 10} height={32} rx={16} />
                        )}
                        <rect class="mg-hit" x={-ext - 6} y={-14} width={ext * 2 + 12} height={28} rx={14} />
                        <rect
                          class="mg-pill"
                          x={-ext}
                          y={-11}
                          width={ext * 2}
                          height={22}
                          rx={11}
                          fill="url(#mg-fill-entity)"
                          filter="url(#mg-node-shadow)"
                          opacity={alpha}
                        />
                        <text class="mg-pill-label" dominant-baseline="central" text-anchor="middle">
                          {fitText(n.label)}
                        </text>
                      </>
                    ) : (
                      <>
                        {selected === n.id && <circle class="mg-halo" r={r + 6} />}
                        <circle class="mg-hit" r={r + 10} />
                        {n.status && n.status !== 'active' && <circle class="mg-ring" r={r + 4.5} />}
                        <circle
                          class="mg-dot"
                          r={r}
                          fill={`url(#mg-fill-${KIND_FILL[n.kind] ? n.kind : 'core'})`}
                          filter="url(#mg-node-shadow)"
                          opacity={alpha}
                        />
                        <text y={r + 13} text-anchor="middle">
                          {lines.length === 1 ? (
                            lines[0]
                          ) : (
                            lines.map((ln, li) => (
                              <tspan key={li} x="0" dy={li === 0 ? 0 : 12}>
                                {ln}
                              </tspan>
                            ))
                          )}
                        </text>
                      </>
                    )}
                  </g>
                </g>
              )
            })}
          </g>
        </svg>
      )}
      {steps.length > 1 && (
        <div class="mg-timeline">
          <button
            type="button"
            class="icon-btn mg-play"
            aria-label={playing ? t('memory.graph.pause') : t('memory.graph.play')}
            aria-pressed={playing}
            onClick={() => setPlaying((p) => !p)}
          >
            {playing ? '❚❚' : '▶'}
          </button>
          <input
            class="mg-range"
            type="range"
            min={0}
            max={steps.length - 1}
            step={1}
            value={stepIdx}
            aria-label={t('memory.graph.timeline')}
            onInput={(e) => {
              setPlaying(false)
              const i = Number((e.target as HTMLInputElement).value)
              setUntil(i >= steps.length - 1 ? '' : (steps[i] ?? ''))
            }}
          />
          <span class="mg-date mono dim">{effectiveUntil}</span>
        </div>
      )}
      <div class="mg-legend">
        {chips.map((k) => (
          <button
            key={k}
            type="button"
            class={`mg-legend-item ${KIND_CLASS[k] ?? 'mg-core'}${hiddenKinds.has(k) ? ' off' : ''}`}
            aria-pressed={!hiddenKinds.has(k)}
            onClick={() => toggleKind(k)}
          >
            <i /> {t(`memory.graph.${k}`, k)}
          </button>
        ))}
        {edgeLegend.map((k) => (
          <span key={k} class={`mg-legend-item mg-legend-edge-${k}`}>
            <svg width="22" height="8">
              <line x1="0" y1="4" x2="22" y2="4" />
            </svg>
            {t(`memory.graph.${k}`)}
          </span>
        ))}
      </div>
      {sel && (
        <div class={`mg-detail card mg-detail-${sel.kind}`}>
          <div class="mg-detail-head">
            <span
              class={`badge ${
                sel.kind === 'core' || sel.kind === ENTITY_KIND
                  ? 'green'
                  : sel.kind === 'project'
                    ? 'blue'
                    : sel.kind === 'skill'
                      ? 'yellow'
                      : ''
              }`}
            >
              {t(`memory.graph.${sel.kind}`, sel.kind)}
            </span>
            <strong class="mono">{sel.label}</strong>
            {sel.status && <span class="badge yellow">{sel.status}</span>}
            <button type="button" class="icon-btn" onClick={() => setSelected(null)} aria-label={t('common.close')}>
              ×
            </button>
          </div>
          {(sel.entries || sel.files || sel.chars || sel.weight) && (
            <div class="mg-detail-stats dim">
              {sel.entries ? <span>{t('memory.graph.entriesN', { n: sel.entries })}</span> : null}
              {sel.files ? <span>{t('memory.graph.filesN', { n: sel.files })}</span> : null}
              {sel.chars ? <span>{t('memory.graph.charsN', { n: sel.chars })}</span> : null}
              {sel.weight && isEntity(sel) ? <span>{t('memory.graph.mentionsN', { n: sel.weight })}</span> : null}
            </div>
          )}
          {(sel.first_seen || sel.last_seen) && (
            <div class="mg-detail-stats dim">
              {sel.first_seen ? (
                <span>
                  {t('memory.graph.firstSeen')} {sel.first_seen}
                </span>
              ) : null}
              {sel.last_seen ? (
                <span>
                  {t('memory.graph.lastSeen')} {sel.last_seen}
                </span>
              ) : null}
            </div>
          )}
          {isEntity(sel) && (sel.evidence?.length ?? 0) > 0 && (
            <div class="mg-evidence">
              <span class="dim">{t('memory.graph.evidence')}</span>
              <ul>
                {sel.evidence!.map((s, i) => (
                  <li key={i}>{s}</li>
                ))}
              </ul>
            </div>
          )}
          {selEdges.length > 0 ? (
            <ul class="mg-detail-edges">
              {selEdges.map((e, i) => {
                const other = e.from === sel.id ? e.to : e.from
                return (
                  <li key={i}>
                    <span class="dim">{e.kind === 'co' ? '·' : e.from === sel.id ? '→' : '←'}</span>
                    <button type="button" class="mg-neighbor mono" onClick={() => setSelected(other)}>
                      {labelOf(other)}
                    </button>
                    <span class="dim">
                      · {t(`memory.graph.${e.kind}`, e.kind)}
                      {(e.weight ?? 0) > 1 ? ` ×${e.weight}` : ''}
                    </span>
                  </li>
                )
              })}
            </ul>
          ) : (
            <p class="dim">{t('memory.graph.noEdges')}</p>
          )}
          {onOpen && sel.kind !== 'core' && sel.kind !== ENTITY_KIND && (
            <button class="btn small" onClick={() => onOpen(sel)}>
              {t('memory.graph.open')}
            </button>
          )}
        </div>
      )}
    </div>
  )
}

/** Edge thickness follows evidence: a repeated mention or several promotions
 *  draw a heavier line than a stray one. */
function edgeWidth(e: MemoryGraphEdge): number {
  const base = e.kind === 'promotion' ? 1.7 : e.kind === 'mention' ? 1.1 : e.kind === 'co' ? 1 : 1.2
  const bonus = (Math.sqrt(e.weight ?? 1) - 1) * 0.8
  return Math.min(6, base + bonus)
}
