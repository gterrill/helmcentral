// The blog card's replay plan: the passages end to end, each night at anchor a
// short pause, and the camera shots' paces stretching the chosen play length.
// Pure. The card's player (main.ts) walks it frame by frame; the clock and the
// shots are the app's own (lib/replay-clock.ts, lib/replay-camera.ts).

import { trackIndex } from '@/lib/boat-model'
import type { ExportData, ExportPassage, ExportShot } from '@/lib/passage-export'
import { buildTimeline, type TimedSegment, type Timeline } from '@/lib/passages'
import { resolveSigns, SIGN_SHOW_DEFAULT, type ReplaySign, type ResolvedSign } from '@/lib/replay-signs'
import { paceSpans, resolveShots, timelineAtWall, wallAtTimeline, type CameraShot, type PaceSpan, type ResolvedShot } from '@/lib/replay-clock'

export interface Step {
  kind: 'run' | 'night'
  /** Where the step starts on the watched clock, in seconds. */
  at: number
  dur: number
  /** The passage the step belongs to (the one just sailed, for a night). */
  i: number
  /** Real time, Unix seconds, at the start and end of the step. */
  t0: number
  t1: number
  night?: number
  place?: string
  /** For a run: seconds of running (nights left out) before the step. */
  w0: number
}

export interface CardState {
  /** [longitude, latitude], or null before the log has a position. */
  pos: [number, number] | null
  /** Real time, Unix seconds. */
  t: number
  passage: string
  index: number
  night: { n: number; place: string } | null
  done: boolean
  /** Where the replay stands on the shots' timeline (passages end to end). */
  clock: number
  /** Seconds of watching so far with nights left out: the clock the signs run on. */
  run: number
}

export interface Plan {
  steps: Step[]
  /** Seconds of watching, nights included. */
  total: number
  timeline: Timeline
  segments: TimedSegment[][]
  shots: ResolvedShot[]
  /** The signs that fall on a passage, placed on its track. */
  signs: ResolvedSign[]
  spans: PaceSpan[]
  stateAt: (wall: number) => CardState
  /** Seconds of watching from a timeline position to another, nights left out. */
  wallAt: (clock: number) => number
}

export function segmentsOf(p: ExportPassage): TimedSegment[] {
  return p.s.map((seg) => seg.map(([t, lon, lat]) => ({ t, lon, lat })))
}

export function shotsOf(shots: readonly ExportShot[]): CameraShot[] {
  return shots.map((s, i) => ({
    id: String(i), t_ms: s.t, mode: s.m, zoom: s.z, pitch: s.p, bearing_offset_deg: s.b, orbit_deg_per_s: s.o, pace: s.k,
  }))
}

function lastFix(segments: TimedSegment[]): [number, number] | null {
  for (let i = segments.length - 1; i >= 0; i--) {
    const seg = segments[i]
    if (seg.length > 0) return [seg[seg.length - 1].lon, seg[seg.length - 1].lat]
  }
  return null
}

/** The card's signs as the app's rules read them. A sign has no id in the
 * card; its place in the list stands in, padded so the order is stable. */
export function signsOf(data: ExportData): ReplaySign[] {
  return data.signs.map((s, i) => ({
    id: String(i).padStart(5, '0'), t_ms: s.t, title: s.ti, note: s.no, document_id: null, photo_url: s.ph ?? null,
    visibility: s.v, show_s: Number.isFinite(s.s) ? s.s : SIGN_SHOW_DEFAULT,
  }))
}

export function buildPlan(data: ExportData): Plan {
  const segments = data.passages.map(segmentsOf)
  const timeline = buildTimeline(data.passages.map((p, i) => ({
    id: String(i), start: p.a, end: Math.max(p.b, p.a + 1), segments: segments[i],
  })))
  // The passages arrive oldest first, which is the order the timeline keeps.
  const shots = resolveShots(shotsOf(data.shots), timeline)
  const spans = paceSpans(shots)
  const wallAt = (clock: number) => wallAtTimeline(timeline.total, data.play, spans, clock)

  const steps: Step[] = []
  let total = 0
  data.passages.forEach((p, i) => {
    if (p.nb > 0) {
      const prev = data.passages[i - 1]
      const each = Math.min(data.nightSeconds, data.nightCap / p.nb)
      for (let k = 0; k < p.nb; k++) {
        steps.push({
          kind: 'night', at: total, dur: each, i: i - 1, night: p.n0 + k, place: p.pl, w0: 0,
          t0: prev.b + ((p.a - prev.b) * k) / p.nb, t1: prev.b + ((p.a - prev.b) * (k + 1)) / p.nb,
        })
        total += each
      }
    }
    const entry = timeline.entries[i]
    const w0 = wallAt(entry.offset)
    const dur = wallAt(entry.offset + (entry.end - entry.start)) - w0
    steps.push({ kind: 'run', at: total, dur, i, t0: p.a, t1: p.b, w0 })
    total += dur
  })

  const stepAt = (clock: number) => {
    for (let i = steps.length - 1; i >= 0; i--) if (clock >= steps[i].at) return steps[i]
    return steps[0]
  }

  const stateAt = (wall: number): CardState => {
    const c = Math.max(0, Math.min(total, wall))
    const s = stepAt(c)
    const p = data.passages[s.i]
    const entry = timeline.entries[s.i]
    let t: number
    let clock: number
    let run: number
    if (s.kind === 'night') {
      const f = s.dur > 0 ? Math.min(1, (c - s.at) / s.dur) : 1
      t = s.t0 + (s.t1 - s.t0) * f
      clock = entry.offset + (entry.end - entry.start)
      run = wallAt(clock)
    } else {
      run = s.w0 + (c - s.at)
      clock = timelineAtWall(timeline.total, data.play, spans, run)
      t = Math.max(p.a, Math.min(p.b, entry.start + (clock - entry.offset)))
    }
    const pos = s.kind === 'night' ? lastFix(segments[s.i]) : (() => {
      const at = trackIndex(segments[s.i]).positionAt(t)
      return at ? ([at.lon, at.lat] as [number, number]) : null
    })()
    return {
      pos, t, passage: p.n, index: s.i,
      night: s.kind === 'night' ? { n: s.night as number, place: s.place as string } : null,
      done: c >= total, clock, run,
    }
  }

  return { steps, total, timeline, segments, shots, signs: resolveSigns(signsOf(data), timeline), spans, stateAt, wallAt }
}
