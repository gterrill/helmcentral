// The video of a passage replay, made in the operator's browser (ADR 0247).
//
// The replay is stepped, not recorded: every frame is the shared controller
// asked for one moment, drawn on a plain MapLibre map at the video's size,
// waited for until it is whole, captured, laid over with the signs, and handed
// to the encoder with a timestamp of its own. Nothing runs against the clock, so
// a slow machine makes a slow video, never a wrong one.
//
// Nothing here knows about React. The page hands in the input and a signal, and
// gets a Blob or an error that says what stopped it.

import { worldImageryVisibleAtZoom } from '@/lib/world-imagery'
import { introStartCamera, MERCATOR } from '@/lib/replay-intro'
import { ReplayController, type FrameInput } from '@/lib/replay-controller'
import { zoomBucket, type Camera } from '@/lib/replay-camera'
import { playedIds } from '@/lib/replay-time'
import { closingCardLayout } from '@/lib/passage-video/closing'
import { createCanvasCompositor, type Compositor } from '@/lib/passage-video/draw'
import { checkEncoderSupport, createVideoSink, type VideoSink } from '@/lib/passage-video/encoder'
import { PassageVideoError, cancelled } from '@/lib/passage-video/errors'
import { buildOverlay } from '@/lib/passage-video/overlay'
import { loadPhotos, photoDrawWidth, type PhotoSet } from '@/lib/passage-video/photos'
import { FPS, VIDEO_SIZES, frameSchedule, type VideoSize } from '@/lib/passage-video/schedule'
import type { SceneFrame, SceneSpec, VideoScene } from '@/lib/passage-video/scene'
import { prepareVideoReplay, type PassageVideoInput } from '@/lib/passage-video/setup'
import type { ReplaySign } from '@/lib/replay-signs'

export interface VideoProgress {
  /** Frames handed to the encoder. */
  done: number
  total: number
}

export interface RenderOptions {
  onProgress?: (progress: VideoProgress) => void
  /** Aborted when the operator cancels or leaves the page. */
  signal?: AbortSignal
}

/** Everything the loop leans on that is the browser's, so a test can stand in for it. */
export interface RenderDeps {
  /** The encoder config the browser agreed to, or a throw that says it can't. */
  checkSupport: (size: VideoSize) => Promise<VideoEncoderConfig>
  loadPhotos: (signs: readonly ReplaySign[], width: number, signal?: AbortSignal) => Promise<PhotoSet>
  createSink: (config: VideoEncoderConfig) => Promise<VideoSink>
  createScene: (spec: SceneSpec) => Promise<VideoScene>
  createCompositor: (size: VideoSize, opts: { isDarkTheme: boolean; photos: ReadonlyMap<string, ImageBitmap> }) => Compositor
  fontsReady: () => Promise<unknown>
}

function throwIfCancelled(signal: AbortSignal | undefined) {
  if (signal?.aborted) throw cancelled()
}

/** Waits for `p`, but gives up the moment the signal aborts. */
function raceCancel<T>(p: Promise<T>, signal: AbortSignal | undefined): Promise<T> {
  if (!signal) return p
  return new Promise<T>((resolve, reject) => {
    const onAbort = () => reject(cancelled())
    if (signal.aborted) { onAbort(); return }
    signal.addEventListener('abort', onAbort, { once: true })
    p.then(
      (v) => { signal.removeEventListener('abort', onAbort); resolve(v) },
      (e) => { signal.removeEventListener('abort', onAbort); reject(e) },
    )
  })
}

export function defaultRenderDeps(): RenderDeps {
  return {
    checkSupport: checkEncoderSupport,
    loadPhotos,
    createSink: createVideoSink,
    createScene: async (spec) => (await import('@/lib/passage-video/scene')).createVideoScene(spec),
    createCompositor: createCanvasCompositor,
    fontsReady: () => document.fonts.ready,
  }
}

/** The video of the replay as the page has it set. */
export async function renderPassageVideo(input: PassageVideoInput, options: RenderOptions = {}, deps: RenderDeps = defaultRenderDeps()): Promise<Blob> {
  const { signal, onProgress } = options
  const size = VIDEO_SIZES[input.orientation]
  throwIfCancelled(signal)
  // Before any work: if this browser cannot make the video, say so now.
  const encoderConfig = await deps.checkSupport(size)
  throwIfCancelled(signal)

  // The replay is worked out first: with no track there is nothing to make, and only
  // the signs it places are worth fetching a photo for. A photo that will not load
  // takes its moment out of the time map, so a failure works the replay out again.
  const planned = prepareVideoReplay(input)
  const photos = await deps.loadPhotos(planned.config.signs.map((r) => r.sign), photoDrawWidth(size), signal)
  let scene: VideoScene | null = null
  let sink: VideoSink | null = null
  try {
    throwIfCancelled(signal)
    const setup = photos.failed.size > 0 ? prepareVideoReplay(input, photos.failed) : planned
    const { config } = setup
    const schedule = frameSchedule(config.time)
    await deps.fontsReady()
    sink = await deps.createSink(encoderConfig)
    const start = introStartCamera(setup.bounds)
    const making = deps.createScene({
      width: size.width, height: size.height, isDarkTheme: input.isDarkTheme, satellite: input.satellite, threeD: input.threeD,
      hull: input.hull, pieces: config.pieces, bounds: setup.bounds, start,
    })
    try {
      scene = await raceCancel(making, signal)
    } catch (err) {
      // Cancelled while the map was still being made: take it down when it is.
      void making.then((s) => s.dispose(), () => {})
      throw err
    }
    const compositor = deps.createCompositor(size, { isDarkTheme: input.isDarkTheme, photos: photos.bitmaps })

    // The whole track framed in this size: a portrait frame refits it.
    const framing = scene.framing()
    if (!framing) throw new PassageVideoError('source', "The map couldn't work out where to look for the video. The video wasn't finished.")
    const overview = framing.overview

    const controller = new ReplayController()
    const frameMs = 1000 / FPS
    const introMs = schedule.intro * frameMs
    controller.startIntro(start, overview, 0)
    const bitmaps = new Set(photos.bitmaps.keys())
    const firstSeen = new Map<string, number>()
    const momentSigns = new Map(config.signs.map((r) => [r.sign.id, r]))
    let camera: Camera = start
    let done = 0

    /** Draws one frame whole, hands it to the encoder, and counts it. */
    const make = async (index: number, f: SceneFrame, wall: number) => {
      scene!.show(f)
      await raceCancel(scene!.settle(index, schedule.total), signal)
      const model = buildOverlay({
        frame: f.frame, size, project: (lon, lat) => scene!.project(lon, lat), wall, firstSeen, bitmaps,
        momentSign: f.frame.moment ? momentSigns.get(f.frame.moment.signId) : undefined, boatDot: scene!.boatDot, measure: compositor.measure,
      })
      compositor.frame(scene!.canvas, model)
      await raceCancel(sink!.add(compositor.canvas, index), signal)
      onProgress?.({ done: ++done, total: schedule.total })
      throwIfCancelled(signal)
    }

    const inputFor = (wall: number, nowMs: number, playing: boolean): FrameInput => ({
      wall, nowMs, playing, resume: playing ? 1 : 0, reduced: false, wantHeading: setup.wantHeading, signsOpen: false,
      view: { zoom: zoomBucket(scene!.zoom()) },
    })

    // The fly-in, from the globe.
    for (let i = 0; i < schedule.intro; i++) {
      throwIfCancelled(signal)
      const nowMs = i * frameMs
      const at = controller.introAt(nowMs)!
      const frame = controller.step(config, inputFor(0, nowMs, false))
      camera = at.camera
      // The projection only when it changes: the globe on the first frame.
      await make(i, { camera, projection: i === 0 ? at.projection : null, frame, playedIds: playedIds(config.timeline, 0) }, 0)
    }
    controller.endIntro()

    // The sailing, holds and all. The flat map from its first frame.
    const flat = !input.threeD
    for (let j = 0; j < schedule.sailing; j++) {
      throwIfCancelled(signal)
      const wall = Math.min(j / FPS, config.time.totalWall)
      const nowMs = introMs + j * frameMs
      const frame = controller.step(config, inputFor(wall, nowMs, true))
      const placed = controller.cameraAt({ flat, overview, current: scene.camera() }, nowMs)
      if (placed) camera = placed.camera
      else if (j === 0) camera = overview
      await make(schedule.intro + j, { camera, projection: j === 0 ? MERCATOR : null, frame, playedIds: playedIds(config.timeline, config.time.at(wall).clock) }, wall)
    }

    // The closing card, on the last picture of the map.
    const closing = closingCardLayout(size, input.summary, input.satellite && worldImageryVisibleAtZoom(scene.zoom()), compositor.measure)
    compositor.closing(scene.canvas, closing)
    await raceCancel(sink.add(compositor.canvas, schedule.intro + schedule.sailing, schedule.closing), signal)
    onProgress?.({ done: done += schedule.closing, total: schedule.total })
    throwIfCancelled(signal)
    return await sink.finish()
  } catch (err) {
    sink?.cancel()
    throw err
  } finally {
    scene?.dispose()
    photos.close()
  }
}
