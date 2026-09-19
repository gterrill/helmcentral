import { useEffect, useState } from 'react'
import { PIXEL_SHIFT_AMPLITUDE_PX } from '@/lib/displays'

export interface PixelShiftOffset {
  dx: number
  dy: number
}

const OFF: PixelShiftOffset = { dx: 0, dy: 0 }
const A = PIXEL_SHIFT_AMPLITUDE_PX

// Four minutes per position, sixteen for the full loop back to the origin.
// Deliberately far longer than any dwell (display-select.tsx's default is
// 30s): the shift must not read as movement to someone watching the board,
// only as the image not having sat in one spot all afternoon.
const STEP_MS = 4 * 60 * 1000

const POSITIONS: readonly PixelShiftOffset[] = [
  { dx: 0, dy: 0 },
  { dx: A, dy: 0 },
  { dx: A, dy: A },
  { dx: 0, dy: A },
]

/**
 * Slow four-position shift of the wall board (ADR 0110 §5a), to spare an
 * OLED panel a static board burning in over the course of a day. Consumed
 * only as part of display-shell.tsx's inner-box transform — never a layout
 * property — so it never reflows and never invalidates a tile's own canvas.
 * displayFoldPx (lib/displays.ts) already reserves the room this needs to
 * shift into, on both axes.
 *
 * Returns `{dx: 0, dy: 0}` whenever `enabled` is false, including the
 * instant it turns false mid-cycle: the always-dark wall theme and this
 * shift both exist purely to reduce burn-in risk, so there's nothing to
 * preserve about which position it stopped on.
 */
export function usePixelShift(enabled: boolean): PixelShiftOffset {
  const [index, setIndex] = useState(0)

  useEffect(() => {
    if (!enabled) return
    setIndex(0)
    const id = setInterval(() => {
      setIndex((prev) => (prev + 1) % POSITIONS.length)
    }, STEP_MS)
    return () => clearInterval(id)
  }, [enabled])

  return enabled ? POSITIONS[index] : OFF
}
