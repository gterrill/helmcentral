import { describe, expect, it } from 'vitest'

import { FRAME_TIMEOUT_MS, ignoredMapError, sourceFailureMessage, timeoutMessage } from '@/lib/passage-video/errors'
import { TERRAIN_SOURCE_ID } from '@/lib/terrain'

describe('sourceFailureMessage', () => {
  it('names the satellite imagery, and that the boat may be offline', () => {
    const m = sourceFailureMessage('world-imagery', 'HTTP 502')
    expect(m).toContain('satellite imagery')
    expect(m).toContain('offline')
  })

  it('names the 3D terrain', () => {
    expect(sourceFailureMessage(TERRAIN_SOURCE_ID, 'HTTP 502')).toContain('3D terrain')
  })

  it('names the sea marks', () => {
    expect(sourceFailureMessage('openseamap', 'Failed to fetch')).toContain('sea marks')
  })

  it('names the chart for the basemap', () => {
    expect(sourceFailureMessage('protomaps', 'HTTP 500')).toContain('chart')
  })

  it('gives the problem as it was for anything it cannot name', () => {
    expect(sourceFailureMessage(undefined, 'Style is not done loading')).toContain('Style is not done loading')
    expect(sourceFailureMessage('something-else', 'boom')).toContain('boom')
  })

  it('says the video was not finished, so no half video is taken for a whole one', () => {
    expect(sourceFailureMessage('world-imagery', 'x')).toMatch(/wasn't finished|not finished/)
  })
})

describe('timeoutMessage', () => {
  it('says how long, and where the video had got to', () => {
    expect(FRAME_TIMEOUT_MS).toBe(20_000)
    const m = timeoutMessage(41, 520)
    expect(m).toContain('20 seconds')
    expect(m).toContain('frame 41 of 520')
  })

  it('suggests the likely causes: the boat offline, and the tab out of sight', () => {
    const m = timeoutMessage(1, 10)
    expect(m).toContain('offline')
    expect(m).toContain('in front')
  })
})

describe('ignoredMapError', () => {
  it('ignores a sea mark tile that does not exist: there are none there to draw', () => {
    expect(ignoredMapError('openseamap', 404)).toBe(true)
  })

  it('ignores nothing else', () => {
    expect(ignoredMapError('openseamap', 500)).toBe(false)
    expect(ignoredMapError('world-imagery', 404)).toBe(false)
    expect(ignoredMapError(TERRAIN_SOURCE_ID, 404)).toBe(false)
    expect(ignoredMapError(undefined, 404)).toBe(false)
  })
})
