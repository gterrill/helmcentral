// The replay, without React or a map. Everything that moves with time lives
// here: the clock, the camera shots, the boat's smoothed heading, how far the
// track has been sailed and which signs are up. The live replay calls it from
// requestAnimationFrame with performance.now(); an exporter can call it for
// each frame of a video with a time of its own. Nothing here reads a clock of
// its own.
//
// A frame is worked out without changing anything the controller holds, so a
// render that React throws away leaves no trace. What a frame moves on (the
// smoothed heading, the held heading, the shot the camera is placed against)
// is taken by `commit`, once the frame is the one on screen. `step` does both
// for a caller that has no render to throw away.

import { HeadingSmoother, nextHeading, type HeadingState } from '@/lib/boat-model'
import {
  type Camera, type CameraEnv, type CameraResult, CameraDirector, shotStartState,
} from '@/lib/replay-camera'
import type { DrawnPiece } from '@/lib/passage-legs'
import { INTRO_MS, introProjectionAt, makeIntroPath, type IntroProjection } from '@/lib/replay-intro'
import { lightFor, lightKey, type ReplayLight } from '@/lib/replay-light'
import { MAX_SPEED_SEGMENTS, SPEED_RAMP_DARK, SPEED_RAMP_LIGHT, speedFrameAt, speedTrack, type SpeedFrame, type SpeedTrack, type WakeView } from '@/lib/replay-speed'
import { sunPosition } from '@/lib/sun'
import { timelineAtWall, wallAtTimeline, type PaceSpan, type ResolvedShot } from '@/lib/replay-clock'
import { visibleSigns, type ResolvedSign } from '@/lib/replay-signs'
import { locate, positionAt, sailedAt, type Timeline, type TimelineEntry } from '@/lib/passages'

// ── the clock ────────────────────────────────────────────────────────────

/** The timeline position `dtS` seconds of watching after `clock`, clamped to the replay. */
export function advanceClock(total: number, lengthS: number, spans: readonly PaceSpan[], clock: number, dtS: number): number {
  const wall = wallAtTimeline(total, lengthS, spans, clock) + dtS
  return Math.min(total, timelineAtWall(total, lengthS, spans, wall))
}

/** A replay clock from 0 to `total` timeline seconds, played in `lengthS`
 * seconds of watching: play, pause and seek, advanced by the times the caller
 * passes in. */
export class ReplayPlayer {
  clock = 0
  playing = false
  private last: number | null = null

  constructor(private total: number, private lengthS: number, private spans: () => readonly PaceSpan[]) {}

  /** A new timeline or length. The place is kept; the time since the last tick
   * is not counted across the change. */
  configure(total: number, lengthS: number, nowMs: number) {
    this.total = total
    this.lengthS = lengthS
    this.clock = Math.min(this.clock, total)
    if (this.playing) this.last = nowMs
  }

  /** Back to the start, stopped. */
  rewind() {
    this.clock = 0
    this.playing = false
    this.last = null
  }

  seek(value: number) {
    this.clock = Math.max(0, Math.min(this.total, value))
  }

  /** Plays on from the clock. A clock already at the end ends again on the
   * next tick; rewinding it is the caller's choice. */
  play(nowMs: number) {
    this.playing = true
    this.last = nowMs
  }

  pause() {
    this.playing = false
    this.last = null
  }

  /** Moves the clock on to `nowMs`. The replay stops by itself at its end. */
  tick(nowMs: number): number {
    if (!this.playing || this.total <= 0) return this.clock
    const dt = this.last === null ? 0 : (nowMs - this.last) / 1000
    this.last = nowMs
    this.clock = advanceClock(this.total, this.lengthS, this.spans(), this.clock, dt)
    if (this.clock >= this.total) this.pause()
    return this.clock
  }
}

// ── the sailed track ─────────────────────────────────────────────────────

export interface SailedTo {
  at: number
  end?: { lat: number; lon: number }
}

/** For each passage, the real instant the replay has sailed to; the passage the
 * boat is on ends where the marker stands. Null at the start. */
export function sailedToAt(
  timeline: Timeline,
  clock: number,
  hereId: string | undefined,
  boat: { lat: number; lon: number } | null,
): Map<string, SailedTo> | null {
  if (clock <= 0) return null
  const out = new Map<string, SailedTo>()
  for (const e of timeline.entries) {
    const end = e.id === hereId && boat ? { lat: boat.lat, lon: boat.lon } : undefined
    out.set(e.id, { at: sailedAt(e, clock), end })
  }
  return out
}

// ── the controller ───────────────────────────────────────────────────────

export interface ReplayConfig {
  timeline: Timeline
  lengthS: number
  spans: readonly PaceSpan[]
  /** The camera shots placed on the replay, in order. */
  resolved: readonly ResolvedShot[]
  /** The signs that draw something (a join awaiting its name is left out). */
  signs: ResolvedSign[]
  /** The pieces of track as the map draws them, which the sailed track is coloured on. */
  pieces: readonly DrawnPiece[]
  /** Picks the speed ramp that shows on the map. */
  isDarkTheme: boolean
}

export interface FrameInput {
  /** The replay clock, in timeline seconds. */
  clock: number
  /** Time in milliseconds; only differences matter. */
  nowMs: number
  playing: boolean
  /** Bumped when the operator hands the camera back (Play, a scrub). */
  resume: number
  reduced: boolean
  /** The heading is worked out: a boat model to point, or shots to steer by. */
  wantHeading: boolean
  /** The sign editor is open, so every sign shows as a pin. */
  signsOpen?: boolean
  /** Hours added to the time the sun is worked out for. A development check only; 0 otherwise. */
  sunOffsetH?: number
  /** The map's zoom, which sizes the wake. */
  view: WakeView
}

export interface ReplayFrame {
  here: { entry: TimelineEntry; at: number } | null
  boat: { lat: number; lon: number } | null
  /** The bow's smoothed heading, or null. */
  heading: number | null
  sailedTo: Map<string, SailedTo> | null
  /** Cards on screen, in order. */
  cards: ResolvedSign[]
  /** Signs shown as pins. */
  pins: ResolvedSign[]
  /** The light for the sun over the boat at the track's own time; null without a boat. The same object while the sun has barely moved. */
  light: ReplayLight | null
  /** The sailed track coloured by speed, and the wake; null before the replay has moved, and for a track cut into too many segments to colour. */
  speed: SpeedFrame | null
}

interface Smoothed { clock: number; playing: boolean; id: string | null; raw: number | null; out: number | null }
interface Intro { path: (elapsedMs: number) => Camera; to: Camera; start: number; durationMs: number }
/** What a frame will move the controller on to, once it is committed. */
interface Pending {
  config: ReplayConfig
  input: FrameInput
  lastHeading: HeadingState | null
  smoothed: Smoothed
  smoother: { held: number | null; lastMs: number } | null
  speed: SpeedFrame | null
}

export class ReplayController {
  private readonly director = new CameraDirector()
  private readonly smoother = new HeadingSmoother()
  private lastHeading: HeadingState | null = null
  private smoothed: Smoothed | null = null
  private readonly pending = new WeakMap<ReplayFrame, Pending>()
  /** The frame on screen, which the camera is placed against. */
  private committed: { pending: Pending; frame: ReplayFrame } | null = null
  private intro: Intro | null = null
  private lightCache: ReplayLight | null = null
  /** The speed frame on screen, which the next one keeps its gradients and lines from. */
  private speedPrev: SpeedFrame | null = null
  // Memory of work already done for the same inputs; it holds no state of the replay.
  private memo: { parts: unknown[]; track: SpeedTrack | null; sailed: Map<number, Map<string, SailedTo> | null>; cards: Map<number, ResolvedSign[]> } | null = null

  /** The speeds along the track. A track cut into more segments than the colours take has none, and the replay draws its plain line; that is said once, here, with the count. */
  private trackFor(pieces: readonly DrawnPiece[]): SpeedTrack | null {
    const track = speedTrack(pieces)
    if (!track) {
      const segments = pieces.reduce((n, p) => n + p.segments.length, 0)
      console.warn(`Sailed track is not coloured by speed: ${segments} track segments, and the colours take ${MAX_SPEED_SEGMENTS}`)
    }
    return track
  }

  private memoFor(config: ReplayConfig) {
    const parts = [config.timeline, config.lengthS, config.spans, config.resolved, config.signs, config.pieces]
    if (!this.memo || this.memo.parts.some((v, i) => v !== parts[i])) this.memo = { parts, track: this.trackFor(config.pieces), sailed: new Map(), cards: new Map() }
    return this.memo
  }

  /** The frame at the clock. Changes nothing the controller holds. */
  frameAt(config: ReplayConfig, input: FrameInput): ReplayFrame {
    const { clock, nowMs, playing, reduced } = input
    const here = locate(config.timeline, clock)
    const boat = here ? positionAt(here.entry.segments, here.at) : null

    // The bow follows the course sailed; a held heading carries on only while
    // playback moves forward.
    const raw = input.wantHeading && here ? nextHeading(this.lastHeading, { id: here.entry.id, at: here.at }, here.entry.segments) : null
    const { smoothed, smoother } = this.smoothHeading(clock, playing, nowMs, reduced, here ? here.entry.id : null, raw ? raw.heading : null)

    const memo = this.memoFor(config)
    let sailedTo = memo.sailed.get(clock)
    if (sailedTo === undefined) {
      sailedTo = sailedToAt(config.timeline, clock, here?.entry.id, boat)
      memo.sailed.clear()
      memo.sailed.set(clock, sailedTo)
    }
    // Cards come and go on watched time; before the replay has moved off the
    // start every sign is a pin, and the pins show while the sign editor is open.
    const replaying = clock > 0
    let cards = memo.cards.get(clock)
    if (!cards) {
      cards = replaying ? visibleSigns(config.signs, wallAt(config, clock), (at) => wallAt(config, at)) : []
      memo.cards.clear()
      memo.cards.set(clock, cards)
    }
    const sun = boat && here ? sunPosition((here.at + (input.sunOffsetH ?? 0) * 3600) * 1000, boat.lat, boat.lon) : null
    let light: ReplayLight | null = null
    if (sun) {
      // The light is built only when the rounded sun has changed.
      light = this.lightCache && this.lightCache.key === lightKey(sun) ? this.lightCache : lightFor(sun)
      this.lightCache = light
    }
    const speed = sailedTo && memo.track ? speedFrameAt(config.pieces, memo.track, sailedTo, config.isDarkTheme ? SPEED_RAMP_DARK : SPEED_RAMP_LIGHT, this.speedPrev, input.view) : null
    const frame: ReplayFrame = { here, boat, heading: smoothed.out, sailedTo, cards, pins: !replaying || input.signsOpen ? config.signs : [], light, speed }
    this.pending.set(frame, { config, input, lastHeading: raw, smoothed, smoother, speed })
    return frame
  }

  /** Takes a frame as the one on screen: the held heading, the smoothed
   * heading and the shot the camera is placed against move on to it. */
  commit(frame: ReplayFrame): void {
    const p = this.pending.get(frame)
    if (!p) throw new Error('That frame did not come from this controller')
    this.lastHeading = p.lastHeading
    this.smoothed = p.smoothed
    this.speedPrev = p.speed
    if (p.smoother) this.smoother.apply(p.smoother)
    this.committed = { pending: p, frame }
  }

  /** The frame at the clock, taken at once. */
  step(config: ReplayConfig, input: FrameInput): ReplayFrame {
    const frame = this.frameAt(config, input)
    this.commit(frame)
    return frame
  }

  /** The camera for the frame last committed, against the map's own framing.
   * Null without a boat, or when no shot applies. Called by the map each time
   * the clock moves, so the shots are stepped once per frame. */
  cameraAt(env: CameraEnv, nowMs: number): CameraResult | null {
    const c = this.committed
    if (!c || !c.frame.boat) return null
    const { config, input } = c.pending
    const { boat, heading } = c.frame
    return this.director.frame(
      {
        overview: env.overview,
        flat: env.flat,
        shotStart: (rs) => shotStartState(config.timeline, rs),
        elapsedAt: (rs, at) => wallAt(config, at) - wallAt(config, rs.clock),
      },
      {
        resolved: config.resolved,
        clock: input.clock,
        playing: input.playing,
        nowMs,
        reduced: input.reduced,
        resume: input.resume,
        boat: { lat: boat.lat, lon: boat.lon, heading },
        current: env.current,
      },
    )
  }

  // ── the opening move ───────────────────────────────────────────────────

  /** Begins the opening move at `nowMs`. A duration of 0 lands on `to` at once. */
  startIntro(from: Camera, to: Camera, nowMs: number, durationMs: number = INTRO_MS) {
    this.intro = { path: makeIntroPath(from, to, durationMs), to, start: nowMs, durationMs }
  }

  /** The opening move is over, or a person took the map out of its hands. */
  endIntro() {
    this.intro = null
  }

  get introActive(): boolean {
    return this.intro !== null
  }

  /** The camera and projection `nowMs` into the opening move, and whether it
   * has landed (the landing frame is the move's last camera, on the flat map).
   * Null when there is no move. */
  introAt(nowMs: number): ({ camera: Camera; done: boolean } & IntroProjection) | null {
    const i = this.intro
    if (!i) return null
    const elapsed = nowMs - i.start
    return { camera: i.path(elapsed), ...introProjectionAt(i.to, elapsed, i.durationMs), done: elapsed >= i.durationMs }
  }

  // One smoothed heading serves the boat and the camera, so the model does not
  // swing against the screen. The step is keyed on (clock, playing, passage,
  // raw heading), so a frame with the key already committed returns the value
  // held for it. Pausing keeps the value as it stands. A scrub or seek while
  // paused, a new passage and reduced motion show the exact heading. Resuming
  // eases from the held value, timed from the resume.
  private smoothHeading(clock: number, playing: boolean, nowMs: number, reduced: boolean, id: string | null, raw: number | null): { smoothed: Smoothed; smoother: Pending['smoother'] } {
    const sm = this.smoothed
    if (sm && sm.clock === clock && sm.playing === playing && sm.id === id && sm.raw === raw) return { smoothed: sm, smoother: null }
    const idChanged = !!sm && sm.id !== id
    const moved = !sm || sm.clock !== clock || sm.raw !== raw
    // Only a move made while already paused is a scrub; the last tick of a
    // replay moves the clock and stops in the same call, and keeps its ease.
    const snap = reduced || idChanged || (!playing && moved && (!sm || !sm.playing))
    const next = this.smoother.peek(raw, nowMs, { snap, hold: !playing || (!!sm && !sm.playing) })
    return { smoothed: { clock, playing, id, raw, out: next.held }, smoother: next }
  }
}

/** Seconds of watching to reach a clock position. */
export function wallAt(config: Pick<ReplayConfig, 'timeline' | 'lengthS' | 'spans'>, clock: number): number {
  return wallAtTimeline(config.timeline.total, config.lengthS, config.spans, clock)
}
