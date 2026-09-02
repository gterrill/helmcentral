import { fillEchoPixels, type EchoBuffer } from '@/lib/radar-echo/echo-buffer'
import type { EchoLut } from '@/lib/radar-echo/geometry'

/**
 * Canvas side length in device pixels. Its half-side always maps to the
 * current spoke range (see geometry.ts / echo-buffer.ts), so resolution
 * scales with range automatically: 0.9 m/px at 0.25nm, 10.9 m/px at 3nm.
 * 2048px would match `maxSpokeLength` exactly at full range but costs a
 * 16MB texture upload per frame against this size's 4MB.
 */
export const RADAR_ECHO_CANVAS_PX = 1024

export interface EchoCanvas {
  readonly element: HTMLCanvasElement
  /** Fill the backing ImageData from `buf` through `lut`/`palette` and blit it. */
  render(buf: EchoBuffer, lut: EchoLut, palette: Uint32Array): void
}

/**
 * Thin DOM wrapper only: owns the canvas element, its 2D context, and the
 * ImageData backing buffer. jsdom has no 2D canvas context, so nothing in
 * this file is exercised by the test suite -- every module it calls into
 * (the LUT, the palette, the buffer, the fill loop) carries the real logic
 * and is tested there instead. Keep anything added here equally thin, or
 * it becomes untested by construction.
 */
export function createEchoCanvas(sizePx: number = RADAR_ECHO_CANVAS_PX): EchoCanvas {
  const element = document.createElement('canvas')
  element.width = sizePx
  element.height = sizePx

  const ctx = element.getContext('2d')
  if (ctx === null) {
    throw new Error('radar-echo: 2D canvas context unavailable')
  }

  const imageData = ctx.createImageData(sizePx, sizePx)
  const pixels = new Uint32Array(imageData.data.buffer)

  return {
    element,
    render(buf, lut, palette) {
      fillEchoPixels(pixels, buf, lut, palette)
      ctx.putImageData(imageData, 0, 0)
    },
  }
}
