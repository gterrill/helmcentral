// Test helper only, deliberately not `*.test.ts` so Vitest doesn't collect it as a suite.
//
// jsdom's cssstyle library normalises hsl()/hsla() colour values to rgb() when it
// serialises an inline style, both via `.style.<prop>` and in the style attribute's
// own string form. happy-dom keeps whatever colour function the value was written
// in, so `style="stroke: hsl(0 72% 51%)"` under jsdom comes back as
// `style="stroke: rgb(220, 40, 40)"`. A test that reads a colour back off a
// rendered element needs to parse either form to get a comparable result in both
// environments, rather than regex-matching rgb() alone and only ever passing
// under jsdom.
export interface Rgb {
  r: number
  g: number
  b: number
}

function hslToRgb(h: number, s: number, l: number): Rgb {
  const c = (1 - Math.abs(2 * l - 1)) * s
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1))
  const m = l - c / 2
  let r1 = 0
  let g1 = 0
  let b1 = 0
  if (h < 60) {
    r1 = c; g1 = x; b1 = 0
  } else if (h < 120) {
    r1 = x; g1 = c; b1 = 0
  } else if (h < 180) {
    r1 = 0; g1 = c; b1 = x
  } else if (h < 240) {
    r1 = 0; g1 = x; b1 = c
  } else if (h < 300) {
    r1 = x; g1 = 0; b1 = c
  } else {
    r1 = c; g1 = 0; b1 = x
  }
  return {
    r: Math.round((r1 + m) * 255),
    g: Math.round((g1 + m) * 255),
    b: Math.round((b1 + m) * 255),
  }
}

/**
 * Parses a colour out of a CSS value, or a whole serialised `style` attribute
 * string, accepting rgb()/rgba() and hsl()/hsla(), comma or space separated
 * (the modern CSS Color 4 syntax this codebase's severity palette is written
 * in, per lib/severity.ts's `hsl(0 72% 51%)`). Returns null when no colour
 * function is found in the given string.
 */
export function parseCssColor(value: string | null | undefined): Rgb | null {
  if (!value) return null

  const rgbMatch = value.match(/rgba?\(\s*(\d+)[,\s]+(\d+)[,\s]+(\d+)/)
  if (rgbMatch) {
    const [, r, g, b] = rgbMatch
    return { r: Number(r), g: Number(g), b: Number(b) }
  }

  const hslMatch = value.match(
    /hsla?\(\s*(\d+(?:\.\d+)?)(?:deg)?[,\s]+(\d+(?:\.\d+)?)%[,\s]+(\d+(?:\.\d+)?)%/,
  )
  if (hslMatch) {
    const [, h, s, l] = hslMatch
    return hslToRgb(Number(h), Number(s) / 100, Number(l) / 100)
  }

  return null
}
