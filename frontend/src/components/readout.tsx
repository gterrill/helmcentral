import type { GaugeZone } from '@/lib/dashboard-widgets'
import { FLUID_READOUT_FONT, FLUID_UNIT_FONT } from '@/lib/fluid-readout'
import { severityTextClass } from '@/lib/severity'

/**
 * A numeric reading with its unit beside it, shared by the Gauge tile and the
 * Current tile. The structural dash, never a zero: a gauge reading 0 when it
 * means "no data" is the dangerous failure, which AGENTS.md's zero-state rule
 * exists for.
 */
export function Readout({ text, unitLabel, zone, size, fallbackTextClass, unitSizeClass, readoutGapClass, fluid, valueTestId }: {
  text: string | null
  unitLabel: string
  zone: GaugeZone['state'] | null
  size: string
  fallbackTextClass: string
  unitSizeClass?: string
  readoutGapClass?: string
  /** Scale with the box around it (FLUID_READOUT_FONT); `size` and `unitSizeClass` are ignored. */
  fluid?: boolean
  /** Test id for the number itself, so a test can read it apart from the unit. */
  valueTestId?: string
}) {
  return (
    <div
      data-slot="readout"
      data-fluid={fluid ? 'true' : undefined}
      className={`flex items-baseline ${readoutGapClass ?? 'gap-1'} min-w-0`}
      style={fluid ? { fontSize: FLUID_READOUT_FONT } : undefined}
    >
      <span
        data-testid={valueTestId}
        style={fluid ? { fontSize: '1em' } : undefined}
        className={`font-display ${fluid ? '' : size} tabular-nums leading-none tracking-tight truncate ${severityTextClass(zone, fallbackTextClass)}`}>
        {text ?? '--'}
      </span>
      {unitLabel && (
        <span
          style={fluid ? { fontSize: FLUID_UNIT_FONT } : undefined}
          className={`${fluid ? '' : unitSizeClass ?? 'text-xs'} leading-none text-muted-foreground`}
        >
          {unitLabel}
        </span>
      )}
    </div>
  )
}
