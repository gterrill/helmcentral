import { describe, expect, it } from 'vitest'

import { VIDEO_SIZES } from '@/lib/passage-video/schedule'
import { FIT_PADDING, overviewCameraFor, passageMapPadding, type FitMap } from '@/lib/replay-framing'

// A map of the video's size whose fit is MapLibre's own sum: the zoom at which
// the bounds fill the shorter of the two padded sides.
function mapOfSize(width: number, height: number): FitMap {
  return {
    getContainer: () => ({ clientWidth: width, clientHeight: height }),
    cameraForBounds: (b, o) => {
      const spanLon = b[1][0] - b[0][0]
      const spanLat = b[1][1] - b[0][1]
      const w = width - o.padding.left - o.padding.right
      const h = height - o.padding.top - o.padding.bottom
      const zoom = Math.min(Math.log2((w / 512) * (360 / spanLon)), Math.log2((h / 512) * (180 / spanLat)), o.maxZoom)
      return { center: { lng: (b[0][0] + b[1][0]) / 2, lat: (b[0][1] + b[1][1]) / 2 }, zoom }
    },
  }
}

// A track that runs a long way east and west and a little north and south.
const bounds: [number, number, number, number] = [148, -20.3, 149.2, -20]

describe('the overview in the video\'s frame', () => {
  const land = VIDEO_SIZES.landscape
  const port = VIDEO_SIZES.portrait

  it('refits the whole track to the portrait frame: wider is closer in landscape, narrower is farther out in portrait', () => {
    const l = overviewCameraFor(mapOfSize(land.width, land.height), bounds, 50)!
    const p = overviewCameraFor(mapOfSize(port.width, port.height), bounds, 50)!
    expect(p.zoom).toBeLessThan(l.zoom)
    expect(p.center).toEqual(l.center)
  })

  it('keeps every corner of the track inside the padded frame, in both', () => {
    for (const size of [land, port]) {
      const cam = overviewCameraFor(mapOfSize(size.width, size.height), bounds, 50)!
      expect(cam.pitch).toBe(50)
      expect(cam.zoom).toBeGreaterThan(0)
    }
  })

  it('is flat for a flat map', () => {
    const cam = overviewCameraFor(mapOfSize(port.width, port.height), bounds, 0)!
    expect(cam.pitch).toBe(0)
    expect(cam.bearing).toBe(0)
  })

  it('pads the same pixels in either frame', () => {
    expect(FIT_PADDING).toEqual({ top: 48, bottom: 80, left: 48, right: 48 })
  })
})

describe('the passage page map\'s padding', () => {
  const area = (p: { top: number; bottom: number; left: number; right: number }, w: number, h: number) => (w - p.left - p.right) * (h - p.top - p.bottom)

  it('is a small margin, tighter than the export frame, with room for the map buttons and the credit', () => {
    const p = passageMapPadding({ w: 900, h: 416 }, null)
    expect(p.top).toBeLessThan(FIT_PADDING.top)
    expect(p.left).toBeLessThan(FIT_PADDING.left)
    expect(p.right).toBeGreaterThanOrEqual(48)
    expect(p.bottom).toBeLessThan(FIT_PADDING.bottom)
  })

  it('leaves the overview map\'s width clear on a wide map, on the side it sits', () => {
    const none = passageMapPadding({ w: 900, h: 416 }, null)
    expect(passageMapPadding({ w: 900, h: 416 }, 'top-right').right - none.right).toBeGreaterThanOrEqual(176)
    expect(passageMapPadding({ w: 900, h: 416 }, 'bottom-left').left - none.left).toBeGreaterThanOrEqual(176)
  })

  it('leaves its height clear instead on a narrow map, where the width would swallow the track', () => {
    const none = passageMapPadding({ w: 400, h: 416 }, null)
    expect(passageMapPadding({ w: 400, h: 416 }, 'top-right').top - none.top).toBeGreaterThanOrEqual(128)
    expect(passageMapPadding({ w: 400, h: 416 }, 'bottom-right').bottom - none.bottom).toBeGreaterThanOrEqual(128)
    expect(area(passageMapPadding({ w: 400, h: 416 }, 'top-right'), 400, 416)).toBeGreaterThan(0)
  })

  it('is used by the fit when given, so the track fills the room that is left', () => {
    const map = mapOfSize(900, 416)
    const tight = overviewCameraFor(map, bounds, 0, passageMapPadding({ w: 900, h: 416 }, null))!
    const loose = overviewCameraFor(map, bounds, 0)!
    expect(tight.zoom).toBeGreaterThanOrEqual(loose.zoom)
    const withInset = overviewCameraFor(map, bounds, 0, passageMapPadding({ w: 900, h: 416 }, 'top-right'))!
    expect(withInset.zoom).toBeLessThanOrEqual(tight.zoom)
  })
})
