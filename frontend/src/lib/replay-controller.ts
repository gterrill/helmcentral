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
  type Camera, type CameraEnv, type CameraResult, CameraDirector, TRANSITION_S, blendCameras, easeInOut, shotStartState,
} from '@/lib/replay-camera'
import type { DrawnPiece } from '@/lib/passage-legs'
import { INTRO_MS, introProjectionAt, makeIntroPath, type IntroProjection } from '@/lib/replay-intro'
import { lightFor, lightKey, type ReplayLight } from '@/lib/replay-light'
import { MAX_SPEED_SEGMENTS, SPEED_RAMP_DARK, SPEED_RAMP_LIGHT, speedFrameAt, speedTrack, type SpeedFrame, type SpeedTrack, type WakeView } from '@/lib/replay-speed'
import { sunPosition } from '@/lib/sun'
import type { ResolvedShot } from '@/lib/replay-clock'
import { cruiseCamera } from '@/lib/replay-framing'
import { momentView, type HoldAt, type MomentView, type ReplayTime } from '@/lib/replay-time'
import { stayView, type ReplayStay, type StayView } from '@/lib/replay-stay'
import { passageLabelView, type PassageLabelView, type ReplayPassageLabel } from '@/lib/replay-passage-label'
import { MAX_SIGN_CARDS, visibleSigns, type ResolvedSign } from '@/lib/replay-signs'
import { isMultiPassage, locate, positionAt, sailedAt, type Timeline, type TimelineEntry } from '@/lib/passages'

/** Timeline seconds within which a position counts as standing at a moment's clock. */
const CLOCK_EPS = 1e-6

// ── the clock ────────────────────────────────────────────────────────────

/** The replay's place: how many seconds of watching it has run (the sailing
 * and every photo moment's hold), which the time map turns into a position on
 * the timeline and, in a hold, a moment. Play, pause, seek and skip, advanced by
 * the times the caller passes in. */
export class ReplayPlayer {
  /** Seconds of watching from the start. */
  wall = 0
  playing = false
  private last: number | null = null

  private placed: { wall: number; time: ReplayTime; at: ReturnType<ReplayTime['at']> } | null = null

  constructor(private time: ReplayTime) {}

  /** Where the watching is, worked out once for each place it stands at. */
  private get at(): ReturnType<ReplayTime['at']> {
    if (!this.placed || this.placed.wall !== this.wall || this.placed.time !== this.time) {
      this.placed = { wall: this.wall, time: this.time, at: this.time.at(this.wall) }
    }
    return this.placed.at
  }

  /** The timing the place is measured in. */
  get timing(): ReplayTime {
    return this.time
  }

  /** The position on the timeline. */
  get clock(): number {
    return this.at.clock
  }

  /** The hold the replay is in (a photo moment or a stay), or null. */
  get hold(): HoldAt | null {
    return this.at.hold
  }

  /** The replay has run to its end, holds and all. */
  get ended(): boolean {
    return this.wall >= this.time.totalWall
  }

  /** New timing (a shot's pace edited, a sign gaining a moment, a new length).
   * The place is kept: the same position on the timeline, and the same second
   * of the same moment if it is still there. The time since the last tick is
   * not counted across the change. */
  setTime(next: ReplayTime, nowMs: number) {
    if (next === this.time) return
    const old = this.time
    const wall = this.wall
    const here = this.at
    this.time = next
    // A different timeline starts again from the beginning.
    if (next.total !== old.total) {
      this.rewind()
      return
    }
    if (here.hold) {
      // In a hold: the same second of the same one if it is still there, else the start of that place.
      const inHold = here.hold
      const hold = next.holds.find((h) => h.id === inHold.id)
      this.wall = hold ? hold.startWall + Math.min(inHold.elapsed, hold.durationS) : next.wallAtClock(here.clock, { onSign: true })
    } else {
      // Not in a hold, but perhaps standing at the clock of one: past it. A hold that has been seen stays seen.
      // The very start is the exception: nothing has been seen there, and a moment standing at the start is still to come.
      const seen = new Set(old.holds.filter((h) => Math.abs(h.clock - here.clock) < CLOCK_EPS && h.startWall + h.durationS <= wall + CLOCK_EPS).map((h) => h.id))
      const unseen = next.holds.find((h) => Math.abs(h.clock - here.clock) < CLOCK_EPS && !seen.has(h.id))
      // Past some holds here and short of another: at its start. Otherwise clear of every hold at this clock.
      this.wall = wall <= CLOCK_EPS ? 0 : seen.size > 0 && unseen ? unseen.startWall : next.wallAtClock(here.clock)
    }
    if (this.playing) this.last = nowMs
  }

  /** Back to the start, stopped. */
  rewind() {
    this.wall = 0
    this.playing = false
    this.last = null
  }

  /** Over sailing time. Lands clear of every hold, after those of a sign
   * standing at that clock; only a jump `onSign` lands at the start of its moment. */
  seek(clock: number, opts?: { onSign?: boolean; at?: number }) {
    this.wall = this.time.wallAtClock(clock, opts)
  }

  /** Ends the moment the replay is in and carries on from the sign. */
  skipMoment() {
    this.wall = this.time.holdEnd(this.wall)
  }

  /** Plays on. A replay already at the end ends again on the next tick;
   * rewinding it is the caller's choice. */
  play(nowMs: number) {
    this.playing = true
    this.last = nowMs
  }

  pause() {
    this.playing = false
    this.last = null
  }

  /** Moves on to `nowMs`. The replay stops by itself at its end. */
  tick(nowMs: number) {
    if (!this.playing || this.time.totalWall <= 0) return
    const dt = this.last === null ? 0 : (nowMs - this.last) / 1000
    this.last = nowMs
    this.wall = Math.min(this.time.totalWall, this.wall + dt)
    if (this.ended) this.pause()
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
  /** How the watching maps to the timeline: the sailing, the shots' paces, the photo moments and the stays. */
  time: ReplayTime
  /** The stays between passages, which the time map holds the replay for. None for a single passage. */
  stays?: readonly ReplayStay[]
  /** The lower-third naming each passage as it starts. None for a single passage. */
  passageLabels?: readonly ReplayPassageLabel[]
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
  /** Seconds of watching. The timeline position and any photo moment come from it. */
  wall: number
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
  /** The photo moment under way, or null. */
  moment: MomentView | null
  /** The stay between two passages under way, or null. During it the boat sits where the passage before it ended. */
  stay: StayView | null
  /** The hold the replay is held in, a photo moment or a stay: the camera stands still for it. */
  hold: HoldAt | null
  /** The lower-third naming the passage the boat has just set sail on, or null. It holds nothing up. */
  passageLabel: PassageLabelView | null
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
  /** The timeline position of the frame. */
  clock: number
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
  private memo: { parts: unknown[]; track: SpeedTrack | null; sailed: Map<string, Map<string, SailedTo> | null>; cards: Map<number, ResolvedSign[]>; stays: Map<string, ReplayStay>; entries: Map<string, TimelineEntry>; labels: Map<string, { label: ReplayPassageLabel; startWall: number }> } | null = null

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
    const parts = [config.timeline, config.time, config.resolved, config.signs, config.pieces, config.stays, config.passageLabels]
    if (!this.memo || this.memo.parts.some((v, i) => v !== parts[i])) {
      const entries = new Map(config.timeline.entries.map((e) => [e.id, e]))
      this.memo = {
        parts, track: this.trackFor(config.pieces), sailed: new Map(), cards: new Map(),
        stays: new Map((config.stays ?? []).map((s) => [s.id, s])),
        entries,
        labels: new Map((config.passageLabels ?? []).map((l) => [l.id, { label: l, startWall: config.time.wallAtClock(entries.get(l.id)!.offset) }])),
      }
    }
    return this.memo
  }

  /** The frame at the clock. Changes nothing the controller holds. */
  frameAt(config: ReplayConfig, input: FrameInput): ReplayFrame {
    const { nowMs, playing, reduced, wall } = input
    // Where the watching is, once: the timeline position, and the hold if it is in one.
    const at = config.time.at(wall)
    const clock = at.clock
    const memo = this.memoFor(config)
    // A stay: the boat has arrived and sits at the end of the passage before.
    const stay = at.hold?.kind === 'stay' ? memo.stays.get(at.hold.id) : undefined
    const arrived = stay ? memo.entries.get(stay.fromId) : undefined
    // A photo moment is at its sign, in the passage the sign is in: at a join, that is the passage it ends or the one it starts, whichever the clock says.
    const heldSign = at.hold?.kind === 'moment' ? config.signs.find((r) => r.sign.id === at.hold!.id) : undefined
    const signEntry = heldSign ? memo.entries.get(heldSign.entryId) : undefined
    const here = arrived ? { entry: arrived, at: arrived.end } : heldSign && signEntry ? { entry: signEntry, at: heldSign.at } : locate(config.timeline, clock)
    const boat = here ? positionAt(here.entry.segments, here.at) : null

    // The bow follows the course sailed; a held heading carries on only while
    // playback moves forward.
    const raw = input.wantHeading && here ? nextHeading(this.lastHeading, { id: here.entry.id, at: here.at }, here.entry.segments) : null
    const { smoothed, smoother } = this.smoothHeading(clock, playing, nowMs, reduced, here ? here.entry.id : null, raw ? raw.heading : null)

    // At the end of one passage and the start of the next, the clock is the same; where the boat is is not.
    const sailedKey = `${clock}|${here?.entry.id ?? ''}`
    let sailedTo = memo.sailed.get(sailedKey)
    if (sailedTo === undefined) {
      sailedTo = sailedToAt(config.timeline, clock, here?.entry.id, boat)
      memo.sailed.clear()
      memo.sailed.set(sailedKey, sailedTo)
    }
    // Cards come and go on watched time; before the replay has moved off the
    // start every sign is a pin, and the pins show while the sign editor is open.
    const replaying = wall > 0
    // A photo moment: the replay is held at a sign and its card grows, photo and caption with it.
    const inMoment = replaying && at.hold?.kind === 'moment' ? at.hold : null
    const momentSign = inMoment ? config.signs.find((r) => r.sign.id === inMoment.id) : undefined
    const moment = inMoment && momentSign ? momentView(inMoment.id, inMoment.elapsed, reduced) : null
    let cards = memo.cards.get(wall)
    if (!cards) {
      cards = replaying ? visibleSigns(config.signs, wall, (c, r) => config.time.wallAtClock(c, { onSign: true, at: r.at })) : []
      // The card grows from, and shrinks back to, where it stands, so the card is up for the whole moment.
      if (momentSign && !cards.includes(momentSign)) {
        if (cards.length < MAX_SIGN_CARDS) cards = [...cards, momentSign]
        else {
          // Not a third: it takes the place of the card that appeared first (as the rule for a third card does), or of the last if every one is 'always'.
          const give = cards.findIndex((r) => r.sign.visibility !== 'always')
          const at = give >= 0 ? give : cards.length - 1
          cards = cards.map((r, i) => (i === at ? momentSign : r))
        }
      }
      memo.cards.clear()
      memo.cards.set(wall, cards)
    }
    const stayAt = stay && at.hold ? stayView(stay, at.hold.elapsed) : null
    // The light runs with the stay's own clock, from the arrival to the departure.
    const sunTime = stayAt ? stayAt.sunAt : here?.at
    // The passage's label, from the moment its sailing begins (after the fly-in, after a stay and the signs at the departure).
    const named = here && !stayAt && replaying ? memo.labels.get(here.entry.id) : undefined
    const passageLabel = named ? passageLabelView(named.label, wall - named.startWall) : null
    const sun = boat && here && sunTime !== undefined ? sunPosition((sunTime + (input.sunOffsetH ?? 0) * 3600) * 1000, boat.lat, boat.lon) : null
    let light: ReplayLight | null = null
    if (sun) {
      // The light is built only when the rounded sun has changed.
      light = this.lightCache && this.lightCache.key === lightKey(sun) ? this.lightCache : lightFor(sun)
      this.lightCache = light
    }
    const speed = sailedTo && memo.track ? speedFrameAt(config.pieces, memo.track, sailedTo, config.isDarkTheme ? SPEED_RAMP_DARK : SPEED_RAMP_LIGHT, this.speedPrev, input.view) : null
    const frame: ReplayFrame = { here, boat, heading: smoothed.out, sailedTo, cards, pins: !replaying || input.signsOpen ? config.signs : [], light, speed, moment, stay: stayAt, hold: moment || stayAt ? at.hold : null, passageLabel }
    this.pending.set(frame, { config, input, clock, lastHeading: raw, smoothed, smoother, speed })
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
    const shot = this.director.frame(
      {
        overview: env.overview,
        flat: env.flat,
        shotStart: (rs) => shotStartState(config.timeline, rs),
        // By the sailing alone: a moment's hold is no time for the camera to turn.
        elapsedAt: (rs, at) => config.time.sailingWallAt(at) - config.time.sailingWallAt(rs.clock),
      },
      {
        resolved: config.resolved,
        clock: c.pending.clock,
        playing: input.playing,
        nowMs,
        reduced: input.reduced,
        resume: input.resume,
        hold: c.frame.hold !== null,
        boat: { lat: boat.lat, lon: boat.lon, heading },
        current: env.current,
        passageId: c.frame.here?.entry.id,
        inStay: c.frame.stay !== null,
      },
    )
    // A single passage: the shots, or none.
    if (!isMultiPassage(config.timeline) || !env.passages || !c.frame.here) return shot
    // A saved shot frames the boat wherever one applies.
    if (shot && shot.mode !== 'before') return this.remember(shot, input.wall)
    return this.cruiseCameraFor(c, env, input)
  }

  /** The camera in force the last time the camera was asked for, which the next framing eases from. Only a replay of several passages keeps it. */
  private last: { key: string; camera: Camera; wall: number } | null = null
  /** The camera the pull-out of a stay began from, and the framing change being eased. */
  private stayFrom: { key: string; camera: Camera | undefined } | null = null
  private ease: { key: string; from: Camera; startWall: number } | null = null

  private remember(result: CameraResult, wall: number): CameraResult {
    this.last = { key: result.shotKey, camera: result.camera, wall }
    return result
  }

  /** The camera of a replay of several passages when no shot applies: each passage framed on its own, the stays between pulling out to the whole cruise and in again, and the end resting on the whole cruise. A change of framing without one of those moves is eased the way the director eases between shots. */
  private cruiseCameraFor(c: NonNullable<ReplayController['committed']>, env: CameraEnv, input: FrameInput): CameraResult {
    const { config } = c.pending
    const frame = c.frame
    const here = frame.here!
    const stay = frame.stay
    const stayKey = stay ? `stay:${stay.stay.fromId}>${stay.stay.toId}` : null
    // The camera on screen as the stay begins is what it pulls out from.
    if (stayKey && this.stayFrom?.key !== stayKey) this.stayFrom = { key: stayKey, camera: this.last?.camera }
    else if (!stayKey) this.stayFrom = null
    // The last moments of the replay, on the last passage: out to the whole cruise.
    const entries = config.timeline.entries
    const finale = !stay && here.entry.id === entries[entries.length - 1].id
    const endRaw = (input.wall - (config.time.totalWall - TRANSITION_S)) / TRANSITION_S
    const placed = cruiseCamera(
      { overview: env.overview, passages: env.passages! },
      {
        passageId: here.entry.id,
        stay: stay ? { fromId: stay.stay.fromId, toId: stay.stay.toId, progress: stay.progress } : null,
        endProgress: finale && endRaw > 0 ? Math.min(1, endRaw) : undefined,
      },
      input.reduced,
      this.stayFrom?.camera,
    )
    let camera = placed.camera
    // A passage framing that follows something else (the last passage, a shot that ended, a stay) is eased in, when the replay is playing on from it. A seek cuts.
    if (placed.key.startsWith('passage:') && !input.reduced) {
      const last = this.last
      if (last && last.key !== placed.key && input.playing && input.wall - last.wall >= 0 && input.wall - last.wall <= 0.5) {
        this.ease = { key: placed.key, from: last.camera, startWall: input.wall }
      }
      const ease = this.ease
      if (ease && ease.key === placed.key && input.wall >= ease.startWall && input.wall - ease.startWall < TRANSITION_S) {
        camera = blendCameras(ease.from, placed.camera, easeInOut((input.wall - ease.startWall) / TRANSITION_S))
      }
    }
    return this.remember({ camera, shotKey: placed.key, mode: 'overview' }, input.wall)
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
