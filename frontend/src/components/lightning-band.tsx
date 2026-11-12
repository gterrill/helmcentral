import { useId, type ReactNode } from 'react'

import { LegendSwatch } from '@/components/legend-swatch'
import { cn } from '@/lib/utils'
import {
  LIGHTNING_STEP_LABELS,
  lightningOpacity,
  type LightningBlock,
  type LightningGap,
  type LightningStatus,
} from '@/lib/lightning-bands'

/** Height of the band in chart pixels. */
export const LIGHTNING_BAND_HEIGHT = 7

/** Thin diagonal lines in the lightning token, for hours the forecast has no data for. */
function NoDataPattern({ id }: { id: string }) {
  return (
    <pattern id={id} patternUnits="userSpaceOnUse" width="4" height="4" patternTransform="rotate(45)">
      <line x1="0" y1="0" x2="0" y2="4" stroke="hsl(var(--chart-lightning))" strokeOpacity={0.45} strokeWidth="1" />
    </pattern>
  )
}

interface LightningBandProps {
  testId: string
  blockTestId: string
  /** Test id of each hatched "no data" strip. */
  gapTestId: string
  /** The chart's viewBox, so the blocks sit in the same coordinates as its overlay. */
  chartWidth: number
  chartHeight: number
  /** Top of the band in chart pixels. */
  y: number
  blocks: LightningBlock[]
  /** Stretches of the chart no held window covers. */
  gaps: LightningGap[]
  label: string
  stale?: boolean
}

/**
 * The stepped lightning band across the top of a forecast chart: one block per
 * run of 3-hour windows, in the lightning token at a rising opacity. A window
 * under the first step draws nothing, which says the data is quiet; hours with
 * no window at all are hatched, which says there is no data. It is its own
 * graphic, named for assistive technology, laid over the chart without taking
 * the pointer.
 */
export function LightningBand({ testId, blockTestId, gapTestId, chartWidth, chartHeight, y, blocks, gaps, label, stale = false }: LightningBandProps) {
  const patternId = `${useId()}-no-data`
  return (
    <svg
      data-testid={testId}
      role="img"
      aria-label={label}
      viewBox={`0 0 ${chartWidth} ${chartHeight}`}
      preserveAspectRatio="none"
      className={cn('pointer-events-none absolute inset-0 h-full w-full', stale && 'grayscale')}
    >
      <defs>
        <NoDataPattern id={patternId} />
      </defs>
      {gaps.map((gap) => (
        <rect
          key={gap.key}
          data-testid={gapTestId}
          x={gap.x}
          y={y}
          width={gap.width}
          height={LIGHTNING_BAND_HEIGHT}
          fill={`url(#${patternId})`}
        />
      ))}
      {blocks.map((block) => (
        <rect
          key={block.key}
          data-testid={blockTestId}
          data-step={block.step}
          x={block.x}
          y={y}
          width={block.width}
          height={LIGHTNING_BAND_HEIGHT}
          fill="hsl(var(--chart-lightning))"
          fillOpacity={lightningOpacity(block.step)}
        />
      ))}
    </svg>
  )
}

/** The key's "no data" sample: the band's hatch in a small box. */
function NoDataSwatch() {
  const patternId = `${useId()}-swatch`
  return (
    <svg width="14" height="8" viewBox="0 0 14 8" className="inline-block align-middle" aria-hidden="true">
      <defs>
        <NoDataPattern id={patternId} />
      </defs>
      <rect x="2" y="1" width="10" height="6" rx="1" fill={`url(#${patternId})`} />
    </svg>
  )
}

interface LightningKeyProps {
  testId: string
  status: LightningStatus
  /** The provider's id; the Met Office credit shows for its own plugin only. */
  provider: string | null
  /** Windows are held and drawn, so a failure means they are out of date rather than missing. */
  held: boolean
  /** The chart's stale badge, when the feed has stopped updating. */
  staleBadge?: ReactNode
}

const UKMO_PROVIDER = 'ukmo-lightning'

/**
 * The key line under a chart's lightning band. It says what the band is, why
 * the forecast is not drawn (it is for the boat, not a stay, or the chart's
 * times will not read), and, whenever the feed has failed, one line saying
 * so, even beside a band that is drawn from windows already held. A boat with
 * no lightning plugin gets nothing at all.
 */
export function LightningKey({ testId, status, provider, held, staleBadge }: LightningKeyProps) {
  if (status.kind === 'none') return null

  let body: ReactNode
  if (status.kind === 'stay') {
    body = <p>Lightning is for the boat's position, not the stay.</p>
  } else if (status.kind === 'error') {
    body = <p>Lightning unavailable: {status.message}</p>
  } else {
    body = (
      <>
        {status.error ? <p>{held ? 'Lightning not updated' : 'Lightning unavailable'}: {status.error}</p> : null}
        <p>
          Lightning band, the chance of at least one lightning flash within about 20 km in each 3-hour window:{' '}
          {([1, 2, 3] as const).map((step) => (
            <span key={step} className="mr-1.5 inline-flex items-center gap-1 align-middle">
              <LegendSwatch kind="bar" color={`hsl(var(--chart-lightning) / ${lightningOpacity(step)})`} /> {LIGHTNING_STEP_LABELS[step - 1]}
            </span>
          ))}
          <span className="mr-1.5 inline-flex items-center gap-1 align-middle">
            <NoDataSwatch /> no data
          </span>
          {provider === UKMO_PROVIDER ? ' Lightning data © Crown copyright, Met Office, CC BY-SA 4.0.' : ''}
          {staleBadge ? <> {staleBadge}</> : null}
        </p>
      </>
    )
  }

  return (
    <div data-testid={testId} className="mt-1 text-sm text-muted-foreground">
      {body}
    </div>
  )
}
