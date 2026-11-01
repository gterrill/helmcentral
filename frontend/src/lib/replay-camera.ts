// The camera a replay shot asks for. Pure: given the shots, where the replay
// stands and where the boat is, it says where the map camera should be. The
// map applies it each frame; nothing here touches the map.

import { trackHeading, trackIndex } from '@/lib/boat-model'
import type { Timeline } from '@/lib/passages'
import type { CameraMode, ResolvedShot } from '@/lib/replay-clock'

export interface Camera {
  /** [longitude, latitude] */
  center: [number, number]
  zoom: number
  pitch: number
  bearing: number
}

/** What the map says about itself when the replay places the camera. */
export interface CameraEnv {
  /** The map is flat: 3D is off or the terrain failed. */
  flat: boolean
  /** The framing of the whole replayed track. */
  overview: Camera
  /** The map's camera as it is this instant. */
  current: Camera
}

export interface BoatState {
  lat: number
  lon: number
  /** The bow's bearing in degrees, or null when the track has not said. */
  heading: number | null
}

export interface CameraContext {
  /** The framing of the whole replayed track (flat when the map is flat). */
  overview: Camera
  /** The map is flat: 3D is off or the terrain failed. Shots keep their zoom
   * and centre; pitch and bearing stay 0. */
  flat: boolean
  /** The boat as it stood at the shot's own moment, in the shot's own passage,
   * or null when that passage has no position there. */
  shotStart: (rs: ResolvedShot) => BoatState | null
  /** Seconds of watching from the shot's start to a clock position, by the
   * replay's own clock. Used only to place a seek. */
  elapsedAt: (rs: ResolvedShot, clock: number) => number
}

/** How long the camera takes to settle into a shot, in watching seconds. */
export const TRANSITION_S = 1.5

/** The boat at a shot's moment, read inside the passage the shot belongs to so
 * a boundary between passages cannot send it to the wrong one. */
export function shotStartState(timeline: Timeline, rs: ResolvedShot): BoatState | null {
  const entry = timeline.entries.find((e) => e.id === rs.entryId)
  const pos = entry ? trackIndex(entry.segments).positionAt(rs.at) : null
  if (!entry || !pos) return null
  return { lat: pos.lat, lon: pos.lon, heading: trackHeading(entry.segments, rs.at, null) }
}

export interface FrameInput {
  resolved: readonly ResolvedShot[]
  clock: number
  playing: boolean
  /** performance.now() style clock, milliseconds. */
  nowMs: number
  /** Reduced motion: cut to the shot's framing, no easing, no orbit turn. */
  reduced: boolean
  /** Bumped when the operator hands control back (Play, a scrub). */
  resume: number
  /** A photo moment holds the replay: time does not advance for the camera, which stays as it is. */
  hold?: boolean
  /** The boat now, with its held heading. */
  boat: BoatState
  /** The map's camera as it is this instant. */
  current: Camera
}

export interface CameraResult {
  camera: Camera
  /** Names the shot in force, so the map can tell when a new one has begun. */
  shotKey: string
  /** The shot's mode, or 'before' ahead of the first shot. */
  mode: CameraMode | 'before'
}

export const easeInOut = (t: number) => {
  const x = Math.max(0, Math.min(1, t))
  return x < 0.5 ? 2 * x * x : 1 - (-2 * x + 2) ** 2 / 2
}

/** An angle brought into 0..360. */
export const norm360 = (d: number) => ((d % 360) + 360) % 360

/** The short way round from one bearing to another, in -180..180 degrees. */
export const shortTurn = (from: number, to: number) => ((((to - from) % 360) + 540) % 360) - 180

export function blendCameras(a: Camera, b: Camera, t: number): Camera {
  const turn = shortTurn(a.bearing, b.bearing)
  return {
    center: [a.center[0] + (b.center[0] - a.center[0]) * t, a.center[1] + (b.center[1] - a.center[1]) * t],
    zoom: a.zoom + (b.zoom - a.zoom) * t,
    pitch: a.pitch + (b.pitch - a.pitch) * t,
    bearing: norm360(a.bearing + turn * t),
  }
}

function shotTarget(ctx: CameraContext, rs: ResolvedShot, boat: BoatState, startHeading: number, elapsed: number): Camera {
  const { shot } = rs
  if (shot.mode === 'overview') return ctx.overview
  const center: [number, number] = [boat.lon, boat.lat]
  if (ctx.flat) return { center, zoom: shot.zoom, pitch: 0, bearing: 0 }
  if (shot.mode === 'follow') {
    return { center, zoom: shot.zoom, pitch: shot.pitch, bearing: norm360((boat.heading ?? 0) + shot.bearing_offset_deg) }
  }
  return { center, zoom: shot.zoom, pitch: shot.pitch, bearing: norm360(startHeading + shot.orbit_deg_per_s * elapsed) }
}

/** Decides the camera for each frame of the replay. It remembers, per shot
 * activation, how long the shot has been watched, the camera it eased from and
 * the heading it started on, so nothing depends on the replay length or on the
 * paces before it: changing speed, or editing a pace mid-shot, cannot jump the
 * bearing or restart the ease. */
export class CameraDirector {
  private key: string | null = null
  private elapsed = 0
  private blend = 0
  private from: Camera | null = null
  private startHeading = 0
  private lastNow: number | null = null
  private wasPlaying = false
  private lastResume = 0

  frame(ctx: CameraContext, input: FrameInput): CameraResult | null {
    const { resolved, clock, playing, nowMs, reduced, resume, boat, current } = input
    if (resolved.length === 0) {
      this.key = null
      return null
    }
    let index = -1
    for (let i = 0; i < resolved.length; i++) {
      if (resolved[i].clock <= clock) index = i
      else break
    }
    const rs = index >= 0 ? resolved[index] : null
    const key = rs ? rs.shot.id : 'before'
    const dt = playing && !input.hold && this.wasPlaying && this.lastNow !== null ? Math.max(0, Math.min(0.5, (nowMs - this.lastNow) / 1000)) : 0
    this.lastNow = nowMs
    this.wasPlaying = playing

    if (key !== this.key) {
      let start: BoatState | null = null
      if (rs) {
        start = ctx.shotStart(rs)
        if (!start) {
          this.key = null
          return null
        }
      }
      this.key = key
      this.startHeading = start?.heading ?? 0
      this.from = current
      this.blend = 0
      this.elapsed = 0
    } else if (resume !== this.lastResume) {
      this.from = current
      this.blend = 0
    }
    this.lastResume = resume

    if (playing) {
      this.elapsed += dt
      this.blend += dt
    } else if (rs) {
      // A seek places the shot as continuous play would have reached it.
      this.elapsed = ctx.elapsedAt(rs, clock)
    }
    const target = rs ? shotTarget(ctx, rs, boat, this.startHeading, reduced ? 0 : this.elapsed) : ctx.overview
    const mode = rs ? rs.shot.mode : 'before'
    if (!playing || reduced || this.blend >= TRANSITION_S || !this.from) return { camera: target, shotKey: key, mode }
    return { camera: blendCameras(this.from, target, easeInOut(this.blend / TRANSITION_S)), shotKey: key, mode }
  }
}

/** True once a point has left the central `fraction` of a width x height view. */
export function outsideCentral(p: { x: number; y: number }, width: number, height: number, fraction = 0.6): boolean {
  const mx = (width * (1 - fraction)) / 2
  const my = (height * (1 - fraction)) / 2
  return p.x < mx || p.x > width - mx || p.y < my || p.y > height - my
}

export const sameCamera = (a: Camera, b: Camera) =>
  Math.abs(a.center[0] - b.center[0]) < 1e-7 && Math.abs(a.center[1] - b.center[1]) < 1e-7
  && Math.abs(a.zoom - b.zoom) < 1e-3 && Math.abs(a.pitch - b.pitch) < 0.01
  && Math.abs((((a.bearing - b.bearing) % 360) + 540) % 360 - 180) < 0.01

export interface PlaceInput {
  result: CameraResult
  /** Bumped when the operator hands control back (Play, a scrub). */
  resume: number
  reduced: boolean
  /** The map's camera as it is this instant. */
  current: Camera
  /** Whether the boat has left the central part of the view. Asked only in
   * reduced motion, in a mode that follows her. */
  boatOutside: () => boolean
}

/** Whether the replay or the person holds the camera. The person takes it by
 * dragging, wheeling or pinching (`yield`); a tap does not count. The replay
 * has it back at the next shot, or when Play or a scrub hands control back.
 * The same gate runs the app's map and the blog card's. */
export class CameraGate {
  private yielded = false
  private shotKey: string | null = null
  private resume = 0

  /** A person has taken hold of the map. */
  yield() {
    this.yielded = true
  }

  /** The camera to place now, or null to leave the map where it is. */
  place({ result, resume, reduced, current, boatOutside }: PlaceInput): Camera | null {
    const resumed = this.resume !== resume
    this.resume = resume
    const newShot = this.shotKey !== result.shotKey
    this.shotKey = result.shotKey
    if (resumed || newShot) this.yielded = false
    if (this.yielded) return null
    if (reduced && !(resumed || newShot)) {
      // No continuous motion: cut again only when the boat nears the edge,
      // and only in a mode that follows her.
      if (result.mode !== 'follow' && result.mode !== 'orbit') return null
      if (!boatOutside()) return null
    }
    // Never re-place a camera that is already there.
    return sameCamera(current, result.camera) ? null : result.camera
  }
}
