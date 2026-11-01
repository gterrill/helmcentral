// How many frames the video has and where each one goes in time. Pure.

import { exportFileName, type ExportData } from '@/lib/passage-export'
import { INTRO_MS } from '@/lib/replay-intro'
import type { ReplayTime } from '@/lib/replay-time'

export type VideoOrientation = 'landscape' | 'portrait'

export interface VideoSize {
  orientation: VideoOrientation
  width: number
  height: number
  /** What the operator chooses between. */
  label: string
}

export const VIDEO_SIZES: Record<VideoOrientation, VideoSize> = {
  landscape: { orientation: 'landscape', width: 1920, height: 1080, label: 'Landscape (1920 × 1080)' },
  portrait: { orientation: 'portrait', width: 1080, height: 1920, label: 'Portrait (1080 × 1920)' },
}

export const FPS = 30
/** Seconds the closing card is held. */
export const CLOSING_S = 3

export interface FrameSchedule {
  /** Frames of the opening fly-in. */
  intro: number
  /** Frames of the sailing, holds for photo moments included, ending on the frame that lands on the end. */
  sailing: number
  /** Frames of the closing card. */
  closing: number
  total: number
}

/** The frames of the whole video for a replay's timing. */
export function frameSchedule(time: ReplayTime): FrameSchedule {
  const intro = Math.ceil((INTRO_MS / 1000) * FPS - 1e-6)
  // The sailing runs from 0 to the end of the watching, and the last frame is the one that stands on the end.
  const sailing = Math.ceil(time.totalWall * FPS - 1e-6) + 1
  const closing = CLOSING_S * FPS
  return { intro, sailing, closing, total: intro + sailing + closing }
}

/** Where frame `index` sits in the video, in whole microseconds. Each is
 * worked from the index, not added up, so the rounding cannot drift. */
export function frameTimestampUs(index: number): number {
  return Math.floor((index * 1_000_000) / FPS)
}

/** How long frame `index` shows: up to the next. */
export function frameDurationUs(index: number): number {
  return frameTimestampUs(index + 1) - frameTimestampUs(index)
}

/** The file name: the passage title and the day it started, as the other exports are named. */
export function videoFileName(summary: Pick<ExportData, 'title' | 'start' | 'zone'>): string {
  return exportFileName(summary.title, summary.start, summary.zone.o, 'mp4')
}
