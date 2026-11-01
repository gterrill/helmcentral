import { describe, expect, it, vi } from 'vitest'

import { PassageVideoError, UNSUPPORTED_MESSAGE } from '@/lib/passage-video/errors'
import type { OverlayModel } from '@/lib/passage-video/overlay'
import type { PhotoSet } from '@/lib/passage-video/photos'
import type { Compositor } from '@/lib/passage-video/draw'
import { renderPassageVideo, type RenderDeps } from '@/lib/passage-video/render'
import { FPS, VIDEO_SIZES, frameSchedule } from '@/lib/passage-video/schedule'
import type { SceneFrame, SceneSpec, VideoScene } from '@/lib/passage-video/scene'
import type { VideoSink } from '@/lib/passage-video/encoder'
import { prepareVideoReplay, type PassageVideoInput } from '@/lib/passage-video/setup'
import type { PassageDetail } from '@/lib/passages'
import type { Camera } from '@/lib/replay-camera'
import { INTRO_START_ZOOM } from '@/lib/replay-intro'
import type { ReplaySign } from '@/lib/replay-signs'
import { MOMENT_TOTAL_S } from '@/lib/replay-time'

const t0 = Date.UTC(2026, 9, 24, 0, 0) / 1000
const passage = (): PassageDetail => ({
  id: 'p1', start: new Date(t0 * 1000).toISOString(), end: new Date((t0 + 1000) * 1000).toISOString(),
  from_name: '', to_name: '', title: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '',
  stats: {}, timezone: 'Australia/Brisbane',
  track: [[[-20, 149], [-20, 149.1]]], track_times: [[t0, t0 + 1000]],
  replay_track: [[[-20, 149], [-20, 149.05], [-20, 149.1]]], replay_times: [[t0, t0 + 500, t0 + 1000]],
} as unknown as PassageDetail)
const sign = (id: string, at: number, extra: Partial<ReplaySign> = {}): ReplaySign => ({
  id, t_ms: (t0 + at) * 1000, title: id, note: '', document_id: 'd', photo_url: '/p', visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null, ...extra,
})
const input = (extra: Partial<PassageVideoInput> = {}): PassageVideoInput => ({
  orientation: 'landscape', passages: [passage()], shots: [], signs: [], hull: null, threeD: true, satellite: false, isDarkTheme: false, lengthS: 8,
  summary: { title: 'T', dates: 'D', stats: [{ l: 'Distance', v: '10 nm' }], start: t0, zone: { o: 10, l: 'UTC+10' } }, ...extra,
})

// ── stand-ins ─────────────────────────────────────────────────────────────

function fakes(opts: {
  settle?: (n: number) => Promise<void>
  photos?: Partial<PhotoSet>
  supported?: boolean
  addDelay?: (index: number) => Promise<void>
  framing?: (spec: SceneSpec) => Camera | null
} = {}) {
  const log: string[] = []
  const shown: SceneFrame[] = []
  const added: number[] = []
  const models: (OverlayModel | 'closing')[] = []
  let spec: SceneSpec | null = null
  const state = { sceneDisposed: false, sinkCancelled: false, sinkFinished: false, photosClosed: false, scenes: 0, sinks: 0 }
  let camera: Camera = { center: [149, -20], zoom: 5, pitch: 0, bearing: 0 }

  const scene = (s: SceneSpec): VideoScene => ({
    canvas: { tag: 'map' } as unknown as CanvasImageSource,
    framing: () => {
      const overview = opts.framing ? opts.framing(s) : { center: [149.05, -20] as [number, number], zoom: s.width > s.height ? 11 : 9, pitch: 50, bearing: 0 }
      return overview ? { overview } : null
    },
    camera: () => camera,
    zoom: () => 12,
    boatDot: true,
    project: (lon, lat) => ({ x: (lon - 148) * 100, y: -lat * 20 }),
    show: (f) => { shown.push(f); camera = f.camera },
    settle: async (n) => { await opts.settle?.(n) },
    dispose: () => { state.sceneDisposed = true },
  })
  const deps: RenderDeps = {
    checkSupport: async () => { if (opts.supported === false) throw new PassageVideoError('unsupported', UNSUPPORTED_MESSAGE); log.push('support'); return {} as VideoEncoderConfig },
    loadPhotos: async () => ({ bitmaps: new Map(), failed: new Set(), close: () => { state.photosClosed = true }, ...opts.photos }),
    createSink: async () => {
      state.sinks++
      log.push('sink')
      const sink: VideoSink = {
        add: async (_src, index, count = 1) => { await opts.addDelay?.(index); for (let k = 0; k < count; k++) added.push(index + k) },
        finish: async () => { state.sinkFinished = true; return new Blob(['mp4'], { type: 'video/mp4' }) },
        cancel: () => { state.sinkCancelled = true },
      }
      return sink
    },
    createScene: async (s) => { state.scenes++; spec = s; log.push('scene'); return scene(s) },
    createCompositor: (): Compositor => ({
      canvas: { tag: 'frame' } as unknown as CanvasImageSource,
      measure: (t) => t.length * 7,
      frame: (_map, model) => { models.push(model) },
      closing: () => { models.push('closing') },
    }),
    fontsReady: async () => { log.push('fonts') },
  }
  return { deps, log, shown, added, models, state, spec: () => spec }
}

const expectedFrames = (i: PassageVideoInput, broken: ReadonlySet<string> = new Set()) => frameSchedule(prepareVideoReplay(i, broken).config.time)

describe('renderPassageVideo', () => {
  it('makes every frame of the schedule, in order, and hands back an MP4', async () => {
    const f = fakes()
    const i = input()
    const blob = await renderPassageVideo(i, {}, f.deps)
    const s = expectedFrames(i)
    expect(blob.type).toBe('video/mp4')
    expect(f.added).toHaveLength(s.total)
    expect(f.added).toEqual(Array.from({ length: s.total }, (_, k) => k))
    expect(f.state.sinkFinished).toBe(true)
  })

  it.each([8, 15, 30, 60])('adds up for a %i second replay: the fly-in, the sailing and the closing card', async (lengthS) => {
    const f = fakes()
    const i = input({ lengthS })
    await renderPassageVideo(i, {}, f.deps)
    const s = expectedFrames(i)
    expect(s.sailing).toBe(lengthS * FPS + 1)
    expect(f.added).toHaveLength(s.intro + s.sailing + s.closing)
    expect(f.shown).toHaveLength(s.intro + s.sailing)
    expect(f.models.filter((m) => m === 'closing')).toHaveLength(1)
  })

  it('asks whether the browser can encode before anything is built', async () => {
    const f = fakes({ supported: false })
    const err = await renderPassageVideo(input(), {}, f.deps).catch((e: unknown) => e)
    expect((err as PassageVideoError).kind).toBe('unsupported')
    expect((err as PassageVideoError).message).toBe("This browser can't make H.264 video.")
    expect(f.state.scenes).toBe(0)
    expect(f.state.sinks).toBe(0)
  })

  it('loads the fonts before it draws', async () => {
    const f = fakes()
    await renderPassageVideo(input(), {}, f.deps)
    expect(f.log.indexOf('fonts')).toBeLessThan(f.log.indexOf('scene'))
  })

  describe('the fly-in', () => {
    it('opens on the globe from high over the track and lands on the framing', async () => {
      const f = fakes()
      const i = input()
      await renderPassageVideo(i, {}, f.deps)
      const s = expectedFrames(i)
      const first = f.shown[0]
      expect(first.camera.zoom).toBeCloseTo(INTRO_START_ZOOM, 3)
      expect(first.projection).not.toBeNull()
      expect((first.projection!.type as unknown[])[4]).toBe('vertical-perspective')
      // It comes down onto the framing, then the sailing begins from it.
      const lastIntro = f.shown[s.intro - 1]
      expect(lastIntro.camera.zoom).toBeGreaterThan(first.camera.zoom)
      expect(lastIntro.camera.zoom).toBeLessThanOrEqual(11)
    })

    it('sets the projection when it changes, not every frame, and ends on the flat map', async () => {
      const f = fakes()
      const i = input()
      await renderPassageVideo(i, {}, f.deps)
      const sets = f.shown.map((x) => x.projection).filter((p) => p !== null)
      expect(sets).toHaveLength(2)
      expect(sets[1]).toEqual({ type: 'mercator' })
      expect(f.shown[expectedFrames(i).intro].projection).toEqual({ type: 'mercator' })
    })

    it('shows the replay at its start while it flies: nothing sailed, the signs as pins', async () => {
      const f = fakes()
      await renderPassageVideo(input({ signs: [sign('a', 300)] }), {}, f.deps)
      expect(f.shown[0].frame.sailedTo).toBeNull()
      expect(f.shown[0].frame.pins.map((p) => p.sign.id)).toEqual(['a'])
    })
  })

  describe('the sailing', () => {
    it('runs the replay from its start to its end at 30 frames a second', async () => {
      const f = fakes()
      const i = input({ lengthS: 8 })
      await renderPassageVideo(i, {}, f.deps)
      const s = expectedFrames(i)
      const sailing = f.shown.slice(s.intro)
      expect(sailing).toHaveLength(s.sailing)
      expect(sailing[0].frame.here?.at).toBeCloseTo(t0)
      expect(sailing[sailing.length - 1].frame.here?.at).toBeCloseTo(t0 + 1000)
      const mid = sailing[Math.round(s.sailing / 2) - 1].frame.here!.at
      expect(mid).toBeGreaterThan(t0 + 400)
      expect(mid).toBeLessThan(t0 + 600)
    })

    it('holds for each photo moment: the boat stands still while the photo grows, holds and shrinks', async () => {
      const f = fakes()
      const i = input({ lengthS: 8, signs: [sign('a', 500)], photos: undefined } as Partial<PassageVideoInput>)
      f.deps.loadPhotos = async () => ({ bitmaps: new Map([['a', {} as ImageBitmap]]), failed: new Set(), close: () => {} })
      await renderPassageVideo(i, {}, f.deps)
      const s = expectedFrames(i)
      const sailing = f.shown.slice(s.intro)
      const held = sailing.filter((x) => x.frame.moment !== null)
      expect(held.length).toBeGreaterThanOrEqual(Math.floor(MOMENT_TOTAL_S * FPS) - 1)
      expect(held.length).toBeLessThanOrEqual(Math.ceil(MOMENT_TOTAL_S * FPS) + 1)
      expect(new Set(held.map((x) => x.frame.here!.at)).size).toBe(1)
      expect(held.map((x) => x.frame.moment!.phase)).toEqual([...held.map((x) => x.frame.moment!.phase)].sort((p, q) => ['grow', 'hold', 'shrink'].indexOf(p) - ['grow', 'hold', 'shrink'].indexOf(q)))
      expect(sailing.length).toBe(Math.ceil((8 + MOMENT_TOTAL_S) * FPS - 1e-6) + 1)
    })

    it('fetches no photo when there is no track to make a video of', async () => {
      const f = fakes()
      const spy = vi.fn(f.deps.loadPhotos)
      f.deps.loadPhotos = spy
      const empty = { ...passage(), track: [], track_times: [], replay_track: [], replay_times: [] } as unknown as PassageDetail
      const err = await renderPassageVideo(input({ passages: [empty], signs: [sign('a', 500)] }), {}, f.deps).catch((e: unknown) => e)
      expect((err as PassageVideoError).kind).toBe('source')
      expect(spy).not.toHaveBeenCalled()
      expect(f.state.scenes).toBe(0)
    })

    it('fetches photos only for the signs the replay places, at the size the video draws them', async () => {
      const f = fakes()
      const spy = vi.fn(f.deps.loadPhotos)
      f.deps.loadPhotos = spy
      await renderPassageVideo(input({ lengthS: 8, signs: [sign('on', 500), sign('off', 90_000)] }), {}, f.deps)
      expect(spy).toHaveBeenCalledTimes(1)
      expect((spy.mock.calls[0][0] as ReplaySign[]).map((x) => x.id)).toEqual(['on'])
      expect(spy.mock.calls[0][1]).toBe(Math.ceil(1008 * 1.06))
    })

    it('gives no moment to a photo that would not load', async () => {
      const f = fakes({ photos: { failed: new Set(['a']), bitmaps: new Map() } })
      const i = input({ lengthS: 8, signs: [sign('a', 500)] })
      await renderPassageVideo(i, {}, f.deps)
      expect(f.shown.every((x) => x.frame.moment === null)).toBe(true)
      expect(f.added).toHaveLength(expectedFrames(i, new Set(['a'])).total)
    })

    it('draws the signs over each frame, with the photos that loaded', async () => {
      const f = fakes()
      f.deps.loadPhotos = async () => ({ bitmaps: new Map([['a', {} as ImageBitmap]]), failed: new Set(), close: () => {} })
      await renderPassageVideo(input({ lengthS: 8, signs: [sign('a', 500)] }), {}, f.deps)
      const withCards = f.models.filter((m): m is OverlayModel => m !== 'closing' && m.cards.length > 0)
      expect(withCards.length).toBeGreaterThan(0)
      expect(withCards.some((m) => m.moment !== null)).toBe(true)
      // Photo cards show their photo.
      expect(withCards[0].cards[0].photo).toBe('bitmap')
    })

    it('marks the passages begun so far', async () => {
      const f = fakes()
      await renderPassageVideo(input({ lengthS: 8 }), {}, f.deps)
      expect(f.shown[f.shown.length - 1].playedIds.has('p1')).toBe(true)
    })
  })

  describe('portrait', () => {
    it('refits the whole track to the portrait frame, and keeps the other shots as they are', async () => {
      const seen: { w: number; h: number }[] = []
      const f = fakes({ framing: (s) => { seen.push({ w: s.width, h: s.height }); return { center: [149.05, -20], zoom: s.width > s.height ? 11 : 9, pitch: 50, bearing: 0 } } })
      await renderPassageVideo(input({ orientation: 'portrait' }), {}, f.deps)
      expect(f.spec()).toMatchObject({ width: 1080, height: 1920 })
      expect(seen[0]).toEqual({ w: 1080, h: 1920 })
      // The sailing begins from the portrait framing.
      const s = expectedFrames(input({ orientation: 'portrait' }))
      expect(f.shown[s.intro].camera.zoom).toBe(9)
    })

    it('makes the video the size asked for', async () => {
      const f = fakes()
      await renderPassageVideo(input(), {}, f.deps)
      expect(f.spec()).toMatchObject({ width: VIDEO_SIZES.landscape.width, height: VIDEO_SIZES.landscape.height })
    })
  })

  describe('the closing card', () => {
    it('is held for three seconds after the last frame of the sailing', async () => {
      const f = fakes()
      const i = input()
      await renderPassageVideo(i, {}, f.deps)
      const s = expectedFrames(i)
      expect(f.added.slice(s.intro + s.sailing)).toHaveLength(90)
      expect(f.models[f.models.length - 1]).toBe('closing')
    })
  })

  describe('progress', () => {
    it('counts the frames made, never backwards, to the total', async () => {
      const f = fakes()
      const seen: { done: number; total: number }[] = []
      const i = input()
      await renderPassageVideo(i, { onProgress: (p) => seen.push(p) }, f.deps)
      const s = expectedFrames(i)
      expect(seen[0]).toMatchObject({ done: 1, total: s.total })
      expect(seen[seen.length - 1]).toMatchObject({ done: s.total, total: s.total })
      for (let k = 1; k < seen.length; k++) expect(seen[k].done).toBeGreaterThanOrEqual(seen[k - 1].done)
    })
  })

  describe('cancelling', () => {
    it('stops the loop at once, and lets go of the map, the encoder and the photos', async () => {
      const controller = new AbortController()
      const f = fakes({ addDelay: async (index) => { if (index === 40) controller.abort() } })
      const err = await renderPassageVideo(input(), { signal: controller.signal }, f.deps).catch((e: unknown) => e)
      expect((err as PassageVideoError).kind).toBe('cancelled')
      expect(f.added.length).toBeLessThanOrEqual(42)
      expect(f.state.sceneDisposed).toBe(true)
      expect(f.state.sinkCancelled).toBe(true)
      expect(f.state.photosClosed).toBe(true)
      expect(f.state.sinkFinished).toBe(false)
    })

    it('does not start at all if it was cancelled before it began', async () => {
      const controller = new AbortController()
      controller.abort()
      const f = fakes()
      const err = await renderPassageVideo(input(), { signal: controller.signal }, f.deps).catch((e: unknown) => e)
      expect((err as PassageVideoError).kind).toBe('cancelled')
      expect(f.state.scenes).toBe(0)
      expect(f.state.sinks).toBe(0)
    })

    it('stops a frame that is waiting on the map', async () => {
      const controller = new AbortController()
      const f = fakes({ settle: (n) => (n === 5 ? new Promise(() => {}) : Promise.resolve()) })
      const run = renderPassageVideo(input(), { signal: controller.signal }, f.deps).catch((e: unknown) => e)
      await vi.waitFor(() => expect(f.shown.length).toBe(6))
      controller.abort()
      expect(((await run) as PassageVideoError).kind).toBe('cancelled')
      expect(f.state.sceneDisposed).toBe(true)
    })
  })

  describe('when the map cannot be drawn whole', () => {
    it('stops on a source error, with its message, and takes everything down', async () => {
      const f = fakes({ settle: async (n) => { if (n === 7) throw new PassageVideoError('source', "The satellite imagery didn't load, so the boat may be offline (HTTP 502). The video wasn't finished.") } })
      const err = await renderPassageVideo(input(), {}, f.deps).catch((e: unknown) => e)
      expect((err as PassageVideoError).kind).toBe('source')
      expect((err as PassageVideoError).message).toContain('satellite imagery')
      expect(f.added).toHaveLength(7)
      expect(f.state.sinkFinished).toBe(false)
      expect(f.state.sinkCancelled).toBe(true)
      expect(f.state.sceneDisposed).toBe(true)
      expect(f.state.photosClosed).toBe(true)
    })

    it('stops on a frame that never finishes, and says where', async () => {
      const f = fakes({ settle: async (n) => { if (n === 3) throw new PassageVideoError('timeout', 'The map didn\'t finish drawing within 20 seconds, at frame 3.') } })
      const err = await renderPassageVideo(input(), {}, f.deps).catch((e: unknown) => e)
      expect((err as PassageVideoError).kind).toBe('timeout')
      expect(f.added).toHaveLength(3)
    })

    it('hands back no video, partial or otherwise', async () => {
      const f = fakes({ settle: async (n) => { if (n === 50) throw new PassageVideoError('source', 'x') } })
      await expect(renderPassageVideo(input(), {}, f.deps)).rejects.toBeInstanceOf(PassageVideoError)
    })

    it('stops with the message when the map cannot work out its framing', async () => {
      const f = fakes({ framing: () => null })
      const err = await renderPassageVideo(input(), {}, f.deps).catch((e: unknown) => e)
      expect((err as PassageVideoError).kind).toBe('source')
      expect(f.state.sceneDisposed).toBe(true)
    })
  })

  it('is the same video for the same input, however often it is made', async () => {
    const a = fakes()
    const b = fakes()
    const i = input({ lengthS: 8, signs: [sign('a', 500)] })
    await renderPassageVideo(i, {}, a.deps)
    await renderPassageVideo(i, {}, b.deps)
    expect(a.shown.map((x) => x.camera)).toEqual(b.shown.map((x) => x.camera))
    expect(a.shown.map((x) => x.frame.here?.at)).toEqual(b.shown.map((x) => x.frame.here?.at))
  })
})
