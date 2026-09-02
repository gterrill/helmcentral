/**
 * mayara's `/capabilities` legend and the palette built from it.
 *
 * The legend has 254 entries, not the 252 `pixelValues` implies and not
 * `historyStart: 222` either -- both looked like usable boundaries and
 * neither is (backend/testdata/mayara/spokes-fur6424A.md, and the plan's
 * "Corrections that shape the design"). Build the palette from
 * `pixels[i].color` alone, exactly as mayara's own client does. Indices 254
 * and 255 have no legend entry and are transparent.
 */

export interface MayaraLegendPixel {
  type?: string
  color: string
}

export interface MayaraLegend {
  pixels: MayaraLegendPixel[]
}

export interface RadarCapabilities {
  spokesPerRevolution: number
  maxSpokeLength: number
  legend: MayaraLegend
}

const PALETTE_SIZE = 256

/**
 * Per-field validation, `null` on anything missing or malformed -- no
 * defaults, no partial legend. A radar whose capabilities can't be read
 * cleanly is one whose picture must not be drawn, rather than one drawn
 * with guessed geometry or a truncated colour ramp.
 */
export function parseCapabilities(raw: unknown): RadarCapabilities | null {
  if (typeof raw !== 'object' || raw === null) return null
  const obj = raw as Record<string, unknown>

  if (typeof obj.spokesPerRevolution !== 'number' || !Number.isFinite(obj.spokesPerRevolution)) return null
  if (typeof obj.maxSpokeLength !== 'number' || !Number.isFinite(obj.maxSpokeLength)) return null

  if (typeof obj.legend !== 'object' || obj.legend === null) return null
  const legendObj = obj.legend as Record<string, unknown>
  if (!Array.isArray(legendObj.pixels)) return null

  const pixels: MayaraLegendPixel[] = []
  for (const entry of legendObj.pixels) {
    if (typeof entry !== 'object' || entry === null) return null
    const pixelObj = entry as Record<string, unknown>
    if (typeof pixelObj.color !== 'string' || pixelObj.color === '') return null
    const pixel: MayaraLegendPixel = { color: pixelObj.color }
    if (typeof pixelObj.type === 'string' && pixelObj.type !== '') pixel.type = pixelObj.type
    pixels.push(pixel)
  }

  return {
    spokesPerRevolution: obj.spokesPerRevolution,
    maxSpokeLength: obj.maxSpokeLength,
    legend: { pixels },
  }
}

// Probe the platform's actual byte order for a Uint32Array view over the
// same buffer a canvas ImageData.data (a Uint8ClampedArray) uses, rather
// than assuming. On little-endian, writing 0x0a0b0c0d through the Uint32
// view lands byte 0x0d at byte offset 0; on big-endian it lands 0x0a there
// instead. This is the classic bug in this pattern -- guessing gets every
// echo drawn in a colour that's merely wrong, with nothing that throws.
const IS_LITTLE_ENDIAN = (() => {
  const probe = new Uint8Array(4)
  new Uint32Array(probe.buffer)[0] = 0x0a0b0c0d
  return probe[0] === 0x0d
})()

function packColor(r: number, g: number, b: number, a: number): number {
  return IS_LITTLE_ENDIAN ? (((a << 24) | (b << 16) | (g << 8) | r) >>> 0) : (((r << 24) | (g << 16) | (b << 8) | a) >>> 0)
}

function parseHexColor(color: string): { r: number; g: number; b: number; a: number } | null {
  const hex = color.startsWith('#') ? color.slice(1) : color
  if (hex.length !== 6 && hex.length !== 8) return null
  const r = parseInt(hex.slice(0, 2), 16)
  const g = parseInt(hex.slice(2, 4), 16)
  const b = parseInt(hex.slice(4, 6), 16)
  const a = hex.length === 8 ? parseInt(hex.slice(6, 8), 16) : 255
  if (Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b) || Number.isNaN(a)) return null
  return { r, g, b, a }
}

/**
 * Pack the legend into a 256-entry Uint32Array in the byte layout a
 * Uint32Array view over an ImageData buffer expects. Entries beyond the
 * legend's length (indices 254 and 255, on the real fixture) are left at
 * 0, which is fully transparent regardless of channel order.
 */
export function buildEchoPalette(legend: MayaraLegend): Uint32Array {
  const palette = new Uint32Array(PALETTE_SIZE) // zero-initialised: transparent by default
  for (let i = 0; i < legend.pixels.length && i < PALETTE_SIZE; i++) {
    const parsed = parseHexColor(legend.pixels[i].color)
    if (parsed === null) continue // leave transparent rather than guess at a malformed entry
    palette[i] = packColor(parsed.r, parsed.g, parsed.b, parsed.a)
  }
  return palette
}
