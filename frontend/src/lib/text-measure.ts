// Measuring text the way a canvas will draw it.

import type { Measure } from '@/lib/passage-export-layout'

/** Measures text with a canvas, as it will be drawn. */
export function measureWithCanvas(): Measure {
  const ctx = document.createElement('canvas').getContext('2d')
  if (!ctx) throw new Error('This browser cannot measure text')
  return (text, f) => {
    ctx.font = f
    return ctx.measureText(text).width
  }
}
