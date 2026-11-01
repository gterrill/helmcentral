// What the video is made from, and the replay set up as the page sets it up:
// the same timeline, shots, legs, signs and photo moments, so the video is the
// replay the operator was looking at. (components/passages/replay-panel.tsx does
// the same from its hooks.)

import type { BoatHull } from '@/lib/boat-model'
import type { ExportData } from '@/lib/passage-export'
import { PassageVideoError } from '@/lib/passage-video/errors'
import type { VideoOrientation } from '@/lib/passage-video/schedule'
import type { PassageDetail } from '@/lib/passages'
import { planReplay } from '@/lib/replay-plan'
import type { ReplayConfig } from '@/lib/replay-controller'
import { paceSpans, type CameraShot } from '@/lib/replay-clock'
import { momentsOf, type ReplaySign } from '@/lib/replay-signs'
import { buildReplayTime } from '@/lib/replay-time'

export interface PassageVideoInput {
  orientation: VideoOrientation
  passages: PassageDetail[]
  shots: CameraShot[]
  /** The signs over the passages' span. */
  signs: ReplaySign[]
  hull: BoatHull | null
  threeD: boolean
  satellite: boolean
  isDarkTheme: boolean
  /** The replay's length, in seconds of sailing. */
  lengthS: number
  /** What the closing card says, and what the file is named from. */
  summary: Pick<ExportData, 'title' | 'dates' | 'stats' | 'start' | 'zone'>
}

export interface VideoSetup {
  /** The timeline, time map, shots, signs and pieces, as the controller takes them. */
  config: ReplayConfig
  bounds: [number, number, number, number]
  /** The heading is worked out: a boat model to point, or shots to steer by. */
  wantHeading: boolean
}

/** `brokenPhotos` are the signs whose photo would not load: they get no moment.
 * The replay is built by lib/replay-plan, as the replay panel builds it. */
export function prepareVideoReplay(input: PassageVideoInput, brokenPhotos: ReadonlySet<string> = new Set()): VideoSetup {
  const plan = planReplay(input.passages, input.shots, input.signs, input.hull, input.threeD)
  const { timeline, bounds } = plan
  if (!bounds || timeline.total <= 0) throw new PassageVideoError('source', 'No track was recorded for this passage, so there is nothing to make a video of.')

  const time = buildReplayTime({ total: timeline.total, lengthS: input.lengthS, spans: paceSpans(plan.resolved), moments: momentsOf(plan.placed, brokenPhotos) })
  const config: ReplayConfig = { timeline, time, resolved: plan.resolved, signs: plan.labelled, pieces: plan.pieces, isDarkTheme: input.isDarkTheme }
  return { config, bounds, wantHeading: plan.wantHeading }
}
