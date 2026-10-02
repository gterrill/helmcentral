import { useLayoutEffect, useRef, useState } from 'react'

import { deckPlanUrl, type InventoryDeck, type InventoryZone } from '@/hooks/use-inventory'
import { formatAppLocation } from '@/lib/app-location'
import { useImageSize } from '@/lib/use-image-size'
import { cn } from '@/lib/utils'

// ADR 0156: a deck plan with its locations outlined and its bins pinned.
// The plan image sits in an <svg> whose viewBox is the image's own pixels, so
// stored fractions (0..1) multiply straight into drawing coordinates. Zones
// and pins are real links when the caller gives handlers, coloured from theme
// tokens so day, night and Auto need nothing extra. Labels are sized in plan
// units from the measured scale so they hold a constant on-screen size however
// wide the plan is drawn.
//
// Nothing is drawn until the image has loaded and its size is known; there is
// no default size to draw at.

const NO_CTX = { firstPageId: null }

interface DeckPlanProps {
  deck: InventoryDeck
  zones: InventoryZone[]
  /** The location to draw stronger than the rest. */
  highlightZoneId?: string
  /** The bin whose pin to draw stronger, and label even when pin labels are off. */
  highlightBinId?: string
  /** Makes each outlined location a link; called with the zone id. */
  onOpenZone?: (zoneId: string) => void
  /** Makes each pin a link; called with the bin code. */
  onOpenBin?: (code: string) => void
  /** Label every pin with its code. Off for small plans, where only the highlighted pin is labelled. */
  showPinLabels?: boolean
  className?: string
}

function centroid(points: Array<[number, number]>): [number, number] {
  const n = points.length
  return [points.reduce((s, p) => s + p[0], 0) / n, points.reduce((s, p) => s + p[1], 0) / n]
}

export function DeckPlan({
  deck, zones, highlightZoneId, highlightBinId, onOpenZone, onOpenBin, showPinLabels = true, className,
}: DeckPlanProps) {
  const url = deckPlanUrl(deck)
  const { size, failed } = useImageSize(url)
  const svgRef = useRef<SVGSVGElement>(null)
  const [renderedWidth, setRenderedWidth] = useState(0)

  useLayoutEffect(() => {
    const el = svgRef.current
    if (!el) return
    const measure = () => setRenderedWidth(el.clientWidth)
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    return () => observer.disconnect()
  }, [size])

  if (failed) {
    return <p role="alert" className="text-sm text-destructive">The plan image could not be loaded.</p>
  }
  if (!size) return null

  // Plan units per screen pixel. Zero before the first measure (no layout
  // yet), when 1 keeps the numbers finite rather than collapsing every label.
  const unit = renderedWidth > 0 ? size.width / renderedWidth : 1
  const px = (n: number) => String(Math.round(n * unit * 100) / 100)
  const toPlan = (p: [number, number]): [number, number] => [p[0] * size.width, p[1] * size.height]

  const onPlan = zones.filter((z) => z.deck_id === deck.id && z.polygon && z.polygon.length >= 3)
  const anyHighlight = highlightZoneId !== undefined

  const link = (href: string, label: string, onActivate: () => void, children: React.ReactNode) => (
    <a
      href={href}
      aria-label={label}
      className="group cursor-pointer outline-none"
      onClick={(e) => {
        // Plain left click navigates inside the app; a modified click keeps
        // the browser's own open-in-new-tab behaviour.
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return
        e.preventDefault()
        onActivate()
      }}
    >
      {children}
    </a>
  )

  return (
    <svg
      ref={svgRef}
      data-testid="deck-plan-svg"
      viewBox={`0 0 ${size.width} ${size.height}`}
      className={cn('block h-auto w-full select-none rounded-md border border-border bg-muted', className)}
      role="group"
      aria-label={`${deck.name} plan`}
    >
      <image href={url ?? undefined} x={0} y={0} width={size.width} height={size.height} />

      {onPlan.map((zone) => {
        const pts = zone.polygon!.map(toPlan)
        const highlighted = zone.id === highlightZoneId
        const [cx, cy] = centroid(pts)
        const count = zone.bins.length
        const label = `${zone.name}, ${count} ${count === 1 ? 'bin' : 'bins'}`
        const drawing = (
          <>
            <polygon
              points={pts.map((p) => `${p[0]},${p[1]}`).join(' ')}
              data-highlighted={highlighted ? 'true' : undefined}
              fill={`hsl(var(--primary) / ${highlighted ? 0.3 : anyHighlight ? 0.06 : 0.12})`}
              stroke="hsl(var(--primary))"
              strokeWidth={highlighted ? 3 : 1.5}
              strokeLinejoin="round"
              vectorEffect="non-scaling-stroke"
              className="group-focus-visible:[stroke-width:4]"
            />
            <text
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
          </>
        )
        return onOpenZone ? (
          <g key={zone.id}>
            {link(
              formatAppLocation({ panel: 'inventory', inventorySection: 'locations', locationEditId: zone.id }, NO_CTX),
              label,
              () => onOpenZone(zone.id),
              drawing,
            )}
          </g>
        ) : (
          <g key={zone.id} aria-label={label}>{drawing}</g>
        )
      })}

      {onPlan.flatMap((zone) => zone.bins.filter((b) => b.pin)).map((bin) => {
        const [x, y] = toPlan([bin.pin!.x, bin.pin!.y])
        const highlighted = bin.id === highlightBinId
        const labelled = showPinLabels || highlighted
        const drawing = (
          <>
            {onOpenBin && (
              // A generous invisible target: the visible pin is small and
              // the plan is operated by finger.
              <circle cx={x} cy={y} r={px(14)} fill="transparent" stroke="none" />
            )}
            <circle
              cx={x}
              cy={y}
              r={px(highlighted ? 7 : 5)}
              data-highlighted={highlighted ? 'true' : undefined}
              fill={highlighted ? 'hsl(var(--primary))' : 'hsl(var(--card))'}
              stroke="hsl(var(--primary))"
              strokeWidth={2}
              vectorEffect="non-scaling-stroke"
              className="group-focus-visible:[stroke-width:4]"
            />
            {labelled && (
              <text
                x={x + Number(px(9))}
                y={y}
                dominantBaseline="central"
                fontSize={px(10)}
                fontWeight={600}
                letterSpacing="0.08em"
                fill="hsl(var(--foreground))"
                stroke="hsl(var(--background))"
                strokeWidth={px(3)}
                paintOrder="stroke"
                pointerEvents="none"
              >
                {bin.code}
              </text>
            )}
          </>
        )
        return onOpenBin ? (
          <g key={bin.id}>
            {link(
              formatAppLocation({ panel: 'inventory', inventorySection: 'locations', binCode: bin.code }, NO_CTX),
              `Bin ${bin.code}`,
              () => onOpenBin(bin.code),
              drawing,
            )}
          </g>
        ) : (
          <g key={bin.id} aria-label={`Bin ${bin.code}`}>{drawing}</g>
        )
      })}
    </svg>
  )
}
