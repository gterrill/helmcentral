import { useLayoutEffect, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent } from 'react'

import { Button } from '@/components/ui/button'
import { deckPlanUrl, type InventoryDeck, type InventoryZone } from '@/hooks/use-inventory'
import {
  insertVertex,
  MAX_POINTS,
  MIN_POINTS,
  moveVertex,
  removeVertex,
  withoutZone,
  type LayoutDraft,
  type Point,
} from '@/lib/deck-layout'
import { useImageSize } from '@/lib/use-image-size'
import { cn } from '@/lib/utils'

// ADR 0156: the drawing surface. Hand-written pointer handling over plain SVG
// (works with a finger as well as a mouse); the layout is a controlled draft
// the deck page saves through its Save bar, so a half-drawn outline never
// reaches the server. Loaded lazily through deck-plan-editor.tsx so the wall
// kiosk never parses it.
//
// Modes follow the selection:
//   a location with no outline  - each tap adds a point; tapping the first
//                                 point (or Finish) closes it
//   a location with an outline  - drag its points, tap a midpoint handle to
//                                 add one, Delete removes the selected point
//   a bin of an outlined location - a tap on the plan drops or moves its pin

const NUDGE = 0.005
const NUDGE_FAR = 0.02

export interface DeckPlanEditorProps {
  deck: InventoryDeck
  /** Every location, so one can be put on this deck from anywhere. */
  zones: InventoryZone[]
  /** deck id -> name, for marking a location that is on another deck. */
  deckNames: Record<string, string>
  layout: LayoutDraft
  onChange: (next: LayoutDraft) => void
}

interface Selection {
  zoneId: string
  binId: string | null
}

interface Drag {
  zoneId: string
  index: number
}

const pointsAttr = (pts: Point[], w: number, h: number) => pts.map((p) => `${p[0] * w},${p[1] * h}`).join(' ')

export default function DeckPlanEditorImpl({ deck, zones, deckNames, layout, onChange }: DeckPlanEditorProps) {
  const url = deckPlanUrl(deck)
  const { size, failed } = useImageSize(url)
  const svgRef = useRef<SVGSVGElement>(null)
  const [renderedWidth, setRenderedWidth] = useState(0)
  const [selection, setSelection] = useState<Selection | null>(null)
  const [drawing, setDrawing] = useState<Point[]>([])
  const [vertex, setVertex] = useState<number | null>(null)
  const dragRef = useRef<Drag | null>(null)

  useLayoutEffect(() => {
    const el = svgRef.current
    if (!el) return
    const measure = () => setRenderedWidth(el.clientWidth)
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    return () => observer.disconnect()
  }, [size])

  if (failed) return <p role="alert" className="text-sm text-destructive">The plan image could not be loaded.</p>
  if (!size) return <p className="text-sm text-muted-foreground">Loading plan...</p>

  const unit = renderedWidth > 0 ? size.width / renderedWidth : 1
  const px = (n: number) => Math.round(n * unit * 100) / 100

  const selectedZone = selection ? zones.find((z) => z.id === selection.zoneId) ?? null : null
  const selectedBin = selection?.binId ? selectedZone?.bins.find((b) => b.id === selection.binId) ?? null : null
  const selectedPolygon = selectedZone ? layout.polygons[selectedZone.id] : undefined
  const mode: 'none' | 'draw' | 'edit' | 'pin' = !selectedZone
    ? 'none'
    : selectedBin
      ? 'pin'
      : selectedPolygon
        ? 'edit'
        : 'draw'

  const select = (next: Selection | null) => {
    setSelection(next)
    setDrawing([])
    setVertex(null)
    dragRef.current = null
  }

  const pointFromEvent = (e: { clientX: number; clientY: number }): Point | null => {
    const rect = svgRef.current?.getBoundingClientRect()
    if (!rect || rect.width === 0 || rect.height === 0) return null
    const clamp = (n: number) => Math.min(1, Math.max(0, n))
    return [clamp((e.clientX - rect.left) / rect.width), clamp((e.clientY - rect.top) / rect.height)]
  }

  const setPolygon = (zoneId: string, points: Point[]) => {
    onChange({ ...layout, polygons: { ...layout.polygons, [zoneId]: points } })
  }

  const finishOutline = () => {
    if (!selectedZone || drawing.length < MIN_POINTS) return
    setPolygon(selectedZone.id, drawing)
    setDrawing([])
  }

  const handlePlanPointerDown = (e: ReactPointerEvent<SVGSVGElement>) => {
    if (e.button !== undefined && e.button !== 0) return
    const p = pointFromEvent(e)
    if (!p || !selectedZone) {
      setVertex(null)
      return
    }
    if (mode === 'draw') {
      if (drawing.length < MAX_POINTS) setDrawing([...drawing, p])
    } else if (mode === 'pin' && selectedBin && selectedPolygon) {
      onChange({ ...layout, pins: { ...layout.pins, [selectedBin.id]: { x: p[0], y: p[1] } } })
    } else {
      setVertex(null)
    }
  }

  const handlePointerMove = (e: ReactPointerEvent<SVGSVGElement>) => {
    const drag = dragRef.current
    if (!drag) return
    const p = pointFromEvent(e)
    const points = layout.polygons[drag.zoneId]
    if (!p || !points) return
    setPolygon(drag.zoneId, moveVertex(points, drag.index, p))
  }

  const endDrag = () => { dragRef.current = null }

  const startDrag = (e: ReactPointerEvent<SVGElement>, zoneId: string, index: number) => {
    e.stopPropagation()
    ;(e.currentTarget as Element).setPointerCapture?.(e.pointerId)
    dragRef.current = { zoneId, index }
    setVertex(index)
  }

  const insertAfter = (e: ReactPointerEvent<SVGElement>, zoneId: string, index: number) => {
    const points = layout.polygons[zoneId]
    const a = points[index]
    const b = points[(index + 1) % points.length]
    const next = insertVertex(points, index, [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2])
    if (next === points) {
      e.stopPropagation()
      return
    }
    setPolygon(zoneId, next)
    startDrag(e, zoneId, index + 1)
  }

  const handleKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Escape') {
      if (drawing.length > 0) setDrawing([])
      setVertex(null)
      return
    }
    if (mode === 'draw' && (e.key === 'Backspace' || e.key === 'Delete')) {
      e.preventDefault()
      setDrawing(drawing.slice(0, -1))
      return
    }
    if (mode !== 'edit' || !selectedZone || !selectedPolygon || vertex === null) return
    if (e.key === 'Backspace' || e.key === 'Delete') {
      e.preventDefault()
      const next = removeVertex(selectedPolygon, vertex)
      if (next !== selectedPolygon) {
        setPolygon(selectedZone.id, next)
        setVertex(null)
      }
      return
    }
    const step = e.shiftKey ? NUDGE_FAR : NUDGE
    const delta: Record<string, Point> = {
      ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step],
    }
    const d = delta[e.key]
    if (d) {
      e.preventDefault()
      const at = selectedPolygon[vertex]
      setPolygon(selectedZone.id, moveVertex(selectedPolygon, vertex, [at[0] + d[0], at[1] + d[1]]))
    }
  }

  const outlined = zones.filter((z) => layout.polygons[z.id])
  const handleR = px(8)
  const midR = px(5)

  const zoneNote = (z: InventoryZone) => {
    if (layout.polygons[z.id]) return 'On this plan'
    if (z.deck_id && z.deck_id !== deck.id) return `On ${deckNames[z.deck_id] ?? 'another deck'}`
    return 'Not on a plan'
  }

  return (
    <div className="flex min-w-0 flex-col gap-4 lg:flex-row" onKeyDown={handleKeyDown}>
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <p className="text-sm text-muted-foreground">
          {mode === 'none' && 'Pick a location to outline it, or one of its bins to pin it.'}
          {mode === 'draw' && `Tap the plan to outline ${selectedZone?.name}. Tap the first point or press Finish to close it.`}
          {mode === 'edit' && `Drag a point to reshape ${selectedZone?.name}. Tap a small handle to add a point; select a point and press Delete to remove it.`}
          {mode === 'pin' && (selectedPolygon
            ? `Tap the plan to place ${selectedBin?.code}. Tap again to move it.`
            : `Outline ${selectedZone?.name} on the plan before pinning its bins.`)}
        </p>
        <svg
          ref={svgRef}
          data-testid="deck-plan-editor-svg"
          viewBox={`0 0 ${size.width} ${size.height}`}
          className={cn('block h-auto w-full touch-none select-none rounded-md border border-border bg-muted', mode === 'draw' || mode === 'pin' ? 'cursor-crosshair' : '')}
          role="group"
          aria-label={`${deck.name} plan editor`}
          tabIndex={0}
          onPointerDown={handlePlanPointerDown}
          onPointerMove={handlePointerMove}
          onPointerUp={endDrag}
          onPointerCancel={endDrag}
        >
          <image href={url ?? undefined} x={0} y={0} width={size.width} height={size.height} />

          {outlined.map((zone) => {
            const pts = layout.polygons[zone.id]
            const active = zone.id === selection?.zoneId
            // Outlines other than the selected one stay tappable only while
            // an outline is being edited, so drawing or pinning is never
            // intercepted by the shape underneath.
            const selectable = !active && mode === 'edit'
            return (
              <polygon
                key={zone.id}
                points={pointsAttr(pts, size.width, size.height)}
                data-active={active ? 'true' : undefined}
                fill={`hsl(var(--primary) / ${active ? 0.3 : 0.12})`}
                stroke="hsl(var(--primary))"
                strokeWidth={active ? 3 : 1.5}
                strokeLinejoin="round"
                vectorEffect="non-scaling-stroke"
                aria-label={selectable ? `Select ${zone.name}` : undefined}
                className={selectable ? 'cursor-pointer' : undefined}
                pointerEvents={selectable ? 'all' : 'none'}
                onPointerDown={selectable ? (e) => { e.stopPropagation(); select({ zoneId: zone.id, binId: null }) } : undefined}
              />
            )
          })}

          {outlined.map((zone) => {
            const pts = layout.polygons[zone.id]
            const cx = (pts.reduce((s, p) => s + p[0], 0) / pts.length) * size.width
            const cy = (pts.reduce((s, p) => s + p[1], 0) / pts.length) * size.height
            return (
              <text
                key={zone.id}
                x={cx}
                y={cy}
                textAnchor="middle"
                dominantBaseline="central"
                fontSize={px(12)}
                fontWeight={600}
                fill="hsl(var(--foreground))"
                stroke="hsl(var(--background))"
                strokeWidth={px(3)}
                paintOrder="stroke"
                pointerEvents="none"
              >
                {zone.name}
              </text>
            )
          })}

          {zones.flatMap((z) => (layout.polygons[z.id] ? z.bins : [])).map((bin) => {
            const pin = layout.pins[bin.id]
            if (!pin) return null
            const active = bin.id === selection?.binId
            return (
              <g key={bin.id} pointerEvents="none">
                <circle
                  cx={pin.x * size.width}
                  cy={pin.y * size.height}
                  r={px(active ? 7 : 5)}
                  data-active={active ? 'true' : undefined}
                  fill={active ? 'hsl(var(--primary))' : 'hsl(var(--card))'}
                  stroke="hsl(var(--primary))"
                  strokeWidth={2}
                  vectorEffect="non-scaling-stroke"
                />
                <text
                  x={pin.x * size.width + px(9)}
                  y={pin.y * size.height}
                  dominantBaseline="central"
                  fontSize={px(10)}
                  fontWeight={600}
                  letterSpacing="0.08em"
                  fill="hsl(var(--foreground))"
                  stroke="hsl(var(--background))"
                  strokeWidth={px(3)}
                  paintOrder="stroke"
                >
                  {bin.code}
                </text>
              </g>
            )
          })}

          {mode === 'draw' && drawing.length > 0 && (
            <g>
              <polyline
                points={pointsAttr(drawing, size.width, size.height)}
                fill="none"
                stroke="hsl(var(--primary))"
                strokeWidth={2}
                strokeDasharray="6 4"
                vectorEffect="non-scaling-stroke"
                pointerEvents="none"
              />
              {drawing.map((p, i) => (
                <circle
                  key={i}
                  cx={p[0] * size.width}
                  cy={p[1] * size.height}
                  r={i === 0 ? handleR : px(4)}
                  fill={i === 0 ? 'hsl(var(--background))' : 'hsl(var(--primary))'}
                  stroke="hsl(var(--primary))"
                  strokeWidth={2}
                  vectorEffect="non-scaling-stroke"
                  aria-label={i === 0 ? 'First point, tap to close' : undefined}
                  role={i === 0 ? 'button' : undefined}
                  className={i === 0 ? 'cursor-pointer' : undefined}
                  pointerEvents={i === 0 ? 'all' : 'none'}
                  onPointerDown={i === 0 ? (e) => { e.stopPropagation(); if (drawing.length >= MIN_POINTS) finishOutline() } : undefined}
                />
              ))}
            </g>
          )}

          {mode === 'edit' && selectedZone && selectedPolygon && (
            <g>
              {selectedPolygon.map((a, i) => {
                const b = selectedPolygon[(i + 1) % selectedPolygon.length]
                return (
                  <circle
                    key={`mid-${i}`}
                    cx={((a[0] + b[0]) / 2) * size.width}
                    cy={((a[1] + b[1]) / 2) * size.height}
                    r={midR}
                    fill="hsl(var(--background))"
                    stroke="hsl(var(--primary))"
                    strokeWidth={1.5}
                    vectorEffect="non-scaling-stroke"
                    role="button"
                    aria-label={`Add a point after point ${i + 1} of ${selectedZone.name}`}
                    className="cursor-copy"
                    onPointerDown={(e) => insertAfter(e, selectedZone.id, i)}
                  />
                )
              })}
              {selectedPolygon.map((p, i) => (
                <circle
                  key={`pt-${i}`}
                  cx={p[0] * size.width}
                  cy={p[1] * size.height}
                  r={handleR}
                  fill={i === vertex ? 'hsl(var(--primary))' : 'hsl(var(--background))'}
                  stroke="hsl(var(--primary))"
                  strokeWidth={2}
                  vectorEffect="non-scaling-stroke"
                  role="button"
                  tabIndex={0}
                  aria-label={`Point ${i + 1} of ${selectedZone.name}`}
                  aria-pressed={i === vertex}
                  className="cursor-move outline-none focus-visible:[stroke-width:4]"
                  onPointerDown={(e) => startDrag(e, selectedZone.id, i)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setVertex(i) }
                  }}
                />
              ))}
            </g>
          )}
        </svg>
      </div>

      <div className="flex w-full min-w-0 shrink-0 flex-col gap-3 lg:w-64">
        <ul aria-label="Locations" className="flex flex-col gap-1">
          {zones.map((zone) => {
            const active = zone.id === selection?.zoneId
            return (
              <li key={zone.id} className="flex min-w-0 flex-col gap-1">
                <button
                  type="button"
                  aria-pressed={active && !selection?.binId}
                  onClick={() => select({ zoneId: zone.id, binId: null })}
                  className={cn(
                    'flex min-w-0 flex-col rounded-md border px-3 py-2 text-left text-sm',
                    active ? 'border-primary bg-primary/10' : 'border-border bg-card hover:bg-muted',
                  )}
                >
                  <span className="truncate font-medium text-foreground">{zone.name}</span>
                  <span className="truncate text-xs text-muted-foreground">{zoneNote(zone)}</span>
                </button>
                {active && zone.bins.length > 0 && (
                  <ul aria-label={`Bins in ${zone.name}`} className="ml-3 flex flex-col gap-1">
                    {zone.bins.map((bin) => {
                      const pinned = layout.pins[bin.id] !== undefined
                      const binActive = bin.id === selection?.binId
                      return (
                        <li key={bin.id}>
                          <button
                            type="button"
                            aria-pressed={binActive}
                            onClick={() => select({ zoneId: zone.id, binId: bin.id })}
                            className={cn(
                              'flex w-full min-w-0 items-center justify-between gap-2 rounded-md border px-2 py-1.5 text-left text-xs',
                              binActive ? 'border-primary bg-primary/10' : 'border-border bg-card hover:bg-muted',
                            )}
                          >
                            <span className="truncate font-mono font-medium text-foreground">{bin.code}</span>
                            <span className="shrink-0 text-muted-foreground">{pinned ? 'Pinned' : 'Not pinned'}</span>
                          </button>
                        </li>
                      )
                    })}
                  </ul>
                )}
              </li>
            )
          })}
        </ul>

        <div className="flex flex-wrap gap-2">
          {mode === 'draw' && (
            <>
              <Button type="button" size="sm" disabled={drawing.length < MIN_POINTS} onClick={finishOutline}>Finish outline</Button>
              <Button type="button" size="sm" variant="outline" disabled={drawing.length === 0} onClick={() => setDrawing([])}>Start over</Button>
            </>
          )}
          {mode === 'edit' && selectedZone && (
            <Button type="button" size="sm" variant="outline" onClick={() => { onChange(withoutZone(layout, selectedZone)); select({ zoneId: selectedZone.id, binId: null }) }}>
              Remove from plan
            </Button>
          )}
          {mode === 'pin' && selectedBin && layout.pins[selectedBin.id] && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => {
                const pins = { ...layout.pins }
                delete pins[selectedBin.id]
                onChange({ ...layout, pins })
              }}
            >
              Remove pin
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
