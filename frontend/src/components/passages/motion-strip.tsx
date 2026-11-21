import { memo, type PointerEvent } from 'react'

import type { FeltBand } from '@/lib/passages'
import { severityFill } from '@/lib/severity'

/** A 15-minute spell of motion, drawn as a stretch of the thin strip under the scrubber. */
export interface MotionSpellTick {
  id: string
  /** 0..1 along the scrubber, start and end of the spell. */
  from: number
  to: number
  /** The replay clock the spell starts at: where a tap on it moves the replay. */
  clock: number
  band: FeltBand | null
  /** Says the band and the sway, for the label and the hover text. */
  label: string
}

// Colour is a hint only: the label names the band. The strip borrows the gauge
// ladder: Comfortable is its normal, Lively its warning, Rough its alarm, and a
// band that is not learned yet is neutral.
const BAND_STATE: Record<FeltBand, 'normal' | 'warn' | 'alarm'> = {
  comfortable: 'normal',
  lively: 'warn',
  rough: 'alarm',
}

const fillFor = (band: FeltBand | null) => severityFill(band ? BAND_STATE[band] : null)

const clamp01 = (n: number) => Math.max(0, Math.min(1, n))

/** The spell nearest a point along the strip (0..1): the one it lies in, else the one with the closest edge. */
export function nearestSpell(spells: MotionSpellTick[], at: number): MotionSpellTick | null {
  let best: MotionSpellTick | null = null
  let bestGap = Infinity
  for (const s of spells) {
    const gap = at < s.from ? s.from - at : at > s.to ? at - s.to : 0
    if (gap < bestGap) { best = s; bestGap = gap }
  }
  return best
}

/** One stretch per spell, in a hit area 24 px tall (the bar is 8 px of it), so a
 * thumb can find it. A tap that lands between two narrow spells seeks to the
 * nearest one instead of missing. The strip keeps no clock, so it is drawn once
 * per set of spells and not on every frame of the replay. */
export const MotionStrip = memo(function MotionStrip({ spells, disabled, onSeek }: {
  spells: MotionSpellTick[]
  disabled: boolean
  onSeek: (clock: number) => void
}) {
  // A tap on a spell reaches its own button; only a tap on the strip itself, in
  // the sliver between spells or past either end, is placed here.
  const onPointerUp = (e: PointerEvent<HTMLDivElement>) => {
    if (disabled || e.target !== e.currentTarget) return
    const rect = e.currentTarget.getBoundingClientRect()
    if (rect.width <= 0) return
    const spell = nearestSpell(spells, (e.clientX - rect.left) / rect.width)
    if (spell) onSeek(spell.clock)
  }
  return (
    <div
      role="group"
      aria-label="How the passage felt, by 15 minutes"
      data-testid="motion-strip"
      className="absolute inset-x-0 bottom-0 h-6"
      onPointerUp={onPointerUp}
    >
      {spells.map((s) => (
        <button
          key={s.id}
          type="button"
          aria-label={s.label}
          title={s.label}
          disabled={disabled}
          // The scrubber above already seeks from the keyboard; a day's spells
          // would add dozens of tab stops after it. Still clickable and labelled.
          tabIndex={-1}
          className="group absolute top-0 flex h-full items-end focus-visible:outline-hidden"
          style={{ left: `${clamp01(s.from) * 100}%`, width: `${Math.max(0.5, (clamp01(s.to) - clamp01(s.from)) * 100)}%` }}
          onClick={() => onSeek(s.clock)}
        >
          <span
            aria-hidden="true"
            className="block h-2 w-full rounded-xs border-x border-background group-focus-visible:ring-2 group-focus-visible:ring-ring"
            style={{ backgroundColor: fillFor(s.band) }}
          />
        </button>
      ))}
    </div>
  )
})
