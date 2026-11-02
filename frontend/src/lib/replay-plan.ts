// What a replay is made of, worked out from the passages, the camera shots and
// the signs. The replay panel (components/passages/replay-panel.tsx) and the
// video export (lib/passage-video/setup.ts) both build it here, so the video is
// the replay the operator was looking at and the two cannot drift apart.
//
// The panel calls the steps one at a time, each in its own memo, so a change to
// the shots does not rebuild the timeline; `planReplay` runs them all for a
// caller with no such state.

import type { BoatHull } from '@/lib/boat-model'
import { drawnPieces, passageLegs, type DrawnPiece, type LegPassage } from '@/lib/passage-legs'
import { buildTimeline, segmentBounds, timedSegments, type PassageDetail, type TimedSegment, type Timeline } from '@/lib/passages'
import { passageBoundsOf, type Bounds } from '@/lib/replay-framing'
import { cruisePaceSpans, resolveShots, type CameraShot, type ResolvedShot } from '@/lib/replay-clock'
import { momentsOf, resolveSigns, type ReplaySign, type ResolvedSign } from '@/lib/replay-signs'
import { passageLabelsAtOffset, type ReplayPassageLabel } from '@/lib/replay-passage-label'
import { boatOffsetHours, staysAtOffset, type ReplayStay, type StayPassage } from '@/lib/replay-stay'
import type { ReplayConfig } from '@/lib/replay-controller'
import { buildReplayTime } from '@/lib/replay-time'

/** A passage as the replay reads it: the real times, the track it replays, and the track it draws. */
export interface ReplayItem {
  id: string
  /** Unix seconds. */
  start: number
  end: number
  segments: TimedSegment[]
  line: TimedSegment[]
}

/** Throws if a passage's track and times do not agree. */
export function replayItems(passages: readonly PassageDetail[]): ReplayItem[] {
  return passages.map((p) => ({
    id: p.id,
    start: Date.parse(p.start) / 1000,
    end: Date.parse(p.end) / 1000,
    segments: timedSegments(p.replay_track, p.replay_times),
    line: timedSegments(p.track, p.track_times),
  }))
}

/** The passages' real spans, for working out where the legs split. */
export const legPassages = (items: readonly ReplayItem[]): LegPassage[] => items.map((i) => ({ id: i.id, startMs: i.start * 1000, endMs: i.end * 1000 }))

/** What is drawn: a piece of track per leg. */
export const piecesOf = (items: readonly ReplayItem[], legs?: Parameters<typeof drawnPieces>[1]): DrawnPiece[] =>
  drawnPieces(items.map((i) => ({ id: i.id, segments: i.line })), legs)

/** The shots that play: the ones inside the replayed passages. A shot in a trimmed-out stretch is not played. */
export const playableShots = (shots: readonly CameraShot[], timeline: Timeline): ResolvedShot[] =>
  resolveShots(shots, timeline).filter((r) => timeline.entries.find((e) => e.id === r.entryId)?.segments.some((s) => s.length > 0))

/** The signs that draw something. A join still waiting for its name is listed but draws nothing on the map. */
export const labelledSigns = (placed: readonly ResolvedSign[]): ResolvedSign[] => placed.filter((r) => r.sign.title !== '')

/** The heading is worked out when there is a boat model to point, or shots to steer by. */
export const wantsHeading = (hull: BoatHull | null, threeD: boolean, resolved: readonly ResolvedShot[]): boolean => !!(hull && threeD) || resolved.length > 0

export interface ReplayPlan {
  items: ReplayItem[]
  timeline: Timeline
  /** [west, south, east, north] over the drawn track, or null if there is none. */
  bounds: [number, number, number, number] | null
  pieces: DrawnPiece[]
  resolved: ResolvedShot[]
  /** Every sign inside the passages, with where the boat meets it. */
  placed: ResolvedSign[]
  /** The placed signs that draw something. */
  labelled: ResolvedSign[]
  wantHeading: boolean
  /** The stays between the passages, in order. */
  stays: ReplayStay[]
  /** The lower-third for each passage of a replay of several. */
  passageLabels: ReplayPassageLabel[]
  /** Each passage's own bounds, which the camera frames it by in a replay of several. */
  passageBounds: { id: string; bounds: Bounds | null }[]
}

/** The plan from passages already read into items, with the legs worked out
 * and the zone offset known: what the exported page has, and what `planReplay`
 * builds for the app. The offset is the boat's, in hours east of UTC at the
 * first departure, or null when it is not known. */
export function planFromItems(
  items: ReplayItem[],
  named: readonly StayPassage[],
  legs: Parameters<typeof drawnPieces>[1],
  shots: readonly CameraShot[],
  signs: readonly ReplaySign[],
  hull: BoatHull | null,
  threeD: boolean,
  offsetHours?: number | null,
): ReplayPlan {
  const timeline = buildTimeline(items)
  // The boat's offset, unless the caller carries it.
  const offset = offsetHours !== undefined ? offsetHours : boatOffsetHours(timeline, named)
  const resolved = playableShots(shots, timeline)
  const placed = resolveSigns(signs, timeline)
  return {
    items,
    timeline,
    bounds: segmentBounds(items.flatMap((i) => i.line)),
    pieces: piecesOf(items, legs),
    resolved,
    placed,
    labelled: labelledSigns(placed),
    wantHeading: wantsHeading(hull, threeD, resolved),
    stays: staysAtOffset(timeline, named, offset),
    passageLabels: passageLabelsAtOffset(timeline, named, offset),
    passageBounds: passageBoundsOf(items.map((i) => ({ id: i.id, segments: i.line }))),
  }
}

export function planReplay(passages: readonly PassageDetail[], shots: readonly CameraShot[], signs: readonly ReplaySign[], hull: BoatHull | null, threeD: boolean): ReplayPlan {
  const items = replayItems(passages)
  const legs = items.length > 0 ? passageLegs(legPassages(items), signs) : undefined
  return planFromItems(items, passages, legs, shots, signs, hull, threeD)
}

/** The replay's time and what the controller is given, from a plan: the
 * length the operator chose, the photo moments (less the signs whose photo
 * would not load) and the stays. The video and the exported page both build
 * their replay here. */
export function replayConfigOf(plan: ReplayPlan, lengthS: number, isDarkTheme: boolean, brokenPhotos: ReadonlySet<string> = new Set()): ReplayConfig {
  const { timeline } = plan
  const time = buildReplayTime({ total: timeline.total, lengthS, spans: cruisePaceSpans(plan.resolved, timeline, lengthS), moments: momentsOf(plan.placed, brokenPhotos), stays: plan.stays })
  return { timeline, time, stays: plan.stays, passageLabels: plan.passageLabels, resolved: plan.resolved, signs: plan.labelled, pieces: plan.pieces, isDarkTheme }
}
