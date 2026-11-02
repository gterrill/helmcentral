// The blog card's replay, worked out from what the card carries. The card does
// not keep a clock, a night counter or a camera of its own: it hands its
// passages, shots and signs to the app's own planner (lib/replay-plan), so the
// shots, signs, legs, stays and labels of a page are the replay's. Pure.

import type { BoatHull } from '@/lib/boat-model'
import type { ExportData, ExportPassage, ExportShot } from '@/lib/passage-export'
import { isExportPhoto } from '@/lib/passage-export-signs'
import type { PassageLeg } from '@/lib/passage-legs'
import type { TimedSegment } from '@/lib/passages'
import type { ReplayConfig } from '@/lib/replay-controller'
import type { CameraShot } from '@/lib/replay-clock'
import { planFromItems, replayConfigOf, type ReplayItem, type ReplayPlan } from '@/lib/replay-plan'
import { SIGN_SHOW_DEFAULT, type ReplaySign } from '@/lib/replay-signs'
import type { StayPassage } from '@/lib/replay-stay'

/** The replay names a passage by its place in the card. */
export const passageId = (i: number) => String(i)
/** A sign has no id in the card; its place in the list stands in, padded so the order is stable. */
export const signId = (i: number) => String(i).padStart(5, '0')

/** The boat to draw as a model, as the replay reads it. */
export function hullOf(data: ExportData): BoatHull | null {
  return data.hull ? { type: data.hull.type, loaM: data.hull.loa, beamM: data.hull.beam } : null
}

function segmentsOf(p: ExportPassage): TimedSegment[] {
  return p.s.map((seg) => seg.map(([t, lon, lat]) => ({ t, lon, lat })))
}

function shotsOf(shots: readonly ExportShot[]): CameraShot[] {
  return shots.map((s, i) => ({
    id: String(i), t_ms: s.t, mode: s.m, zoom: s.z, pitch: s.p, bearing_offset_deg: s.b, orbit_deg_per_s: s.o, pace: s.k,
  }))
}

/** The card's signs as the app's rules read them. A sign whose photo does not
 * grow for a moment says so (photo_moment false); one that does leaves it at the
 * default, which is on. */
function signsOf(data: ExportData): ReplaySign[] {
  return data.signs.map((s, i) => ({
    id: signId(i), t_ms: s.t, title: s.ti, note: s.no, document_id: null, photo_url: s.ph ?? null,
    visibility: s.v, show_s: Number.isFinite(s.s) ? s.s : SIGN_SHOW_DEFAULT, starts_leg: false, photo_moment: s.mo ? null : false,
  }))
}

/** The legs, in view order, with the colour index running across every leg of every passage. */
function legsOf(data: ExportData): PassageLeg[] {
  const out: PassageLeg[] = []
  data.passages.forEach((p, i) => {
    for (const leg of p.l) out.push({ passageId: passageId(i), fromMs: leg.a * 1000, toMs: leg.b * 1000, colourIndex: out.length })
  })
  return out
}

export interface CardReplay {
  plan: ReplayPlan
  config: ReplayConfig
}

export function buildReplay(data: ExportData): CardReplay {
  const items: ReplayItem[] = data.passages.map((p, i) => {
    const segments = segmentsOf(p)
    // A passage that starts and ends at one instant still takes a second on the timeline.
    return { id: passageId(i), start: p.a, end: Math.max(p.b, p.a + 1), segments, line: segments }
  })
  const named: StayPassage[] = data.passages.map((p, i) => ({ id: passageId(i), from_name: p.f, to_name: p.t, timezone: null }))
  // The offset is the boat's at the first departure, which the card carries, so the stays count their nights as the cruise page does.
  const plan = planFromItems(items, named, legsOf(data), shotsOf(data.shots), signsOf(data), hullOf(data), data.d3, data.zone.o)
  // A photo the card may not show is drawn without one, so it gets no moment, as in the app and the video when a photo would not load.
  const broken = new Set(data.signs.flatMap((s, i) => (s.ph !== undefined && !isExportPhoto(s.ph) ? [signId(i)] : [])))
  return { plan, config: replayConfigOf(plan, data.play, false, broken) }
}
