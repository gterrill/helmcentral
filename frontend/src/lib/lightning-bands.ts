// The lightning band on the forecast charts: how a window's probability maps
// to a step, and where a window sits on a chart's time axis. Pure, so the
// edges and the mapping are tested without a chart.

import { unknownZoneMessage, zoneKnown } from '@/lib/vessel-time'

/** One 3-hour window of lightning probability at the boat, in epoch milliseconds. */
export interface LightningWindow {
  startMs: number
  endMs: number
  /** Chance of at least one flash in the window, 0 to 1. */
  probability: number
}

/**
 * What the Forecast page knows about the lightning feed. `provider` is null
 * when the last answer named none, which is what a boat with no lightning
 * plugin always gets.
 */
export interface LightningFeed {
  windows: LightningWindow[]
  provider: string | null
  /**
   * Some answer this page load named a provider. Until then the page says
   * nothing about lightning at all, whatever else the feed reports: a boat with
   * no plugin never sees a word, even on an error.
   */
  providerSeen: boolean
  /** The feed's own refresh is running. */
  refreshing: boolean
  /** Why the last refresh failed, or why part of the range is missing. */
  refreshError: string | null
  /** The request itself failed. */
  error: string | null
  updatedAt: string | null
  ttlSeconds: number | null
}

/**
 * What a chart's lightning key has to say. `none` says nothing at all.
 * `ready` draws the band, with the feed's error as a line under it when it has
 * one: windows held across a failed refresh still draw.
 */
export type LightningStatus =
  | { kind: 'none' }
  | { kind: 'stay' }
  | { kind: 'error'; message: string }
  | { kind: 'ready'; error: string | null }

/** The first line of a server error, which is all the key has room for. */
function firstLine(text: string): string {
  return text.split('\n')[0].trim()
}

/**
 * Which of the four things the feed amounts to. Nothing at all until an answer
 * has named a provider. After that a stay only reports that lightning is for
 * the boat, and anything else draws the band, with the first line of any
 * request or refresh error carried beside it. Windows are placed in the
 * forecast's zone, so a zone the boat has not reported, or this browser does
 * not know, is an error rather than a band at the wrong hours.
 */
export function lightningStatus(
  feed: LightningFeed | undefined,
  showingStay: boolean,
  zone: string | undefined,
): LightningStatus {
  // The latest answer decides: a plugin removed mid-session goes quiet again.
  if (!feed || !feed.providerSeen || feed.provider === null) return { kind: 'none' }
  if (showingStay) return { kind: 'stay' }
  if (zone === undefined || !zoneKnown(zone)) {
    return { kind: 'error', message: unknownZoneMessage(zone) ?? 'the boat has not reported its time zone' }
  }
  const failure = feed.error || feed.refreshError
  return { kind: 'ready', error: failure ? firstLine(failure) : null }
}

/** Lower edges of steps 1, 2 and 3. Under the first edge nothing is drawn. */
export const LIGHTNING_STEP_EDGES = [0.1, 0.3, 0.6] as const

export type LightningStep = 0 | 1 | 2 | 3

/** Fill opacity of the lightning token for each step, indexed by step. */
const LIGHTNING_STEP_OPACITY = [0, 0.4, 0.7, 1] as const

/** Step legend labels, for steps 1 to 3. */
export const LIGHTNING_STEP_LABELS = ['10 to 30%', '30 to 60%', '60%+'] as const

/** Which step a probability falls in. A value that is not a probability throws: there is no step to invent for it. */
export function lightningStep(probability: number): LightningStep {
  if (!Number.isFinite(probability) || probability < 0 || probability > 1) {
    throw new RangeError(`lightning probability ${probability} is outside 0 to 1`)
  }
  if (probability >= LIGHTNING_STEP_EDGES[2]) return 3
  if (probability >= LIGHTNING_STEP_EDGES[1]) return 2
  if (probability >= LIGHTNING_STEP_EDGES[0]) return 1
  return 0
}

export function lightningOpacity(step: LightningStep): number {
  return LIGHTNING_STEP_OPACITY[step]
}

/** Reads one window of GET /api/lightning. Throws on a window it cannot read. */
export function parseLightningWindow(api: { start?: unknown; end?: unknown; probability?: unknown }): LightningWindow {
  const startMs = typeof api.start === 'string' ? Date.parse(api.start) : Number.NaN
  const endMs = typeof api.end === 'string' ? Date.parse(api.end) : Number.NaN
  if (!Number.isFinite(startMs) || !Number.isFinite(endMs) || endMs <= startMs) {
    throw new RangeError(`lightning window has no usable times (${String(api.start)} to ${String(api.end)})`)
  }
  const probability = api.probability
  if (typeof probability !== 'number') {
    throw new RangeError('lightning window has no probability')
  }
  lightningStep(probability)
  return { startMs, endMs, probability }
}

/** One drawn block of the band: a run of consecutive slots of one step. */
export interface LightningBlock {
  key: string
  x: number
  width: number
  step: 1 | 2 | 3
}

/** A stretch of the chart no held window covers: drawn hatched, never left blank. */
export interface LightningGap {
  key: string
  x: number
  width: number
}

/**
 * Lays the windows along a chart that draws samples at `sampleMs` (ascending)
 * at pixels `xs`. A window draws the slots of the samples it contains (start
 * included, end excluded). A sample's slot runs halfway to each neighbouring
 * sample and stops at the sample itself at either end of the data, so a window
 * ending as the chart begins draws nothing and one holding only the last
 * sample draws half a slot.
 *
 * Consecutive slots of one step share a block; a quiet (under 10%) slot is
 * "the data says quiet" and breaks a run without drawing. A slot no window
 * holds is "no data" and joins the gaps. A lone sample owns `span` (the
 * plot's width) when it is given, since it has no neighbour to halve with. `peak` is the highest window that
 * holds a sample (the first of equals), with the first sample it holds, which
 * is always a time on the chart.
 */
export function placeLightningOnSamples(
  windows: LightningWindow[],
  sampleMs: number[],
  xs: number[],
  span?: { left: number; right: number },
): { blocks: LightningBlock[]; gaps: LightningGap[]; peak: { window: LightningWindow; atMs: number } | null } {
  if (sampleMs.length !== xs.length) {
    throw new RangeError(`lightning samples (${sampleMs.length}) and positions (${xs.length}) differ in length`)
  }
  const blocks: LightningBlock[] = []
  const gaps: LightningGap[] = []
  let peak: { window: LightningWindow; atMs: number } | null = null
  let openBlock: LightningBlock | null = null
  let openGap: LightningGap | null = null
  const last = sampleMs.length - 1
  for (let idx = 0; idx <= last; idx += 1) {
    const ms = sampleMs[idx]
    const alone = span !== undefined && last === 0
    const left = alone ? span.left : idx === 0 ? xs[0] : (xs[idx - 1] + xs[idx]) / 2
    const right = alone ? span.right : idx === last ? xs[last] : (xs[idx] + xs[idx + 1]) / 2
    const held = lightningAt(windows, ms)
    if (!held) {
      openBlock = null
      if (openGap) openGap.width = right - openGap.x
      else {
        openGap = { key: String(ms), x: left, width: right - left }
        gaps.push(openGap)
      }
      continue
    }
    openGap = null
    if (!peak || held.probability > peak.window.probability) peak = { window: held, atMs: ms }
    const step = lightningStep(held.probability)
    if (step === 0) {
      openBlock = null
    } else if (openBlock && openBlock.step === step) {
      openBlock.width = right - openBlock.x
    } else {
      openBlock = { key: String(ms), x: left, width: right - left, step }
      blocks.push(openBlock)
    }
  }
  return { blocks, gaps, peak }
}

/** The whole band as one "no data" strip, for a chart with no sample to place windows on. */
export function lightningNoDataBand(left: number, width: number): { blocks: LightningBlock[]; gaps: LightningGap[]; label: string } {
  return { blocks: [], gaps: [{ key: 'all', x: left, width }], label: lightningLabel(null, '') }
}

/** The window holding an instant, start included and end excluded. */
export function lightningAt(windows: LightningWindow[], ms: number): LightningWindow | null {
  return windows.find((w) => ms >= w.startMs && ms < w.endMs) ?? null
}

/** A window's chance as a whole percent. */
export function lightningPercent(window: LightningWindow): number {
  return Math.round(window.probability * 100)
}

/** The tooltip line for an instant, or null when no window holds it. A window's chance reads for the hour or sample it falls in, whatever its step. */
export function lightningTip(windows: LightningWindow[], ms: number): string | null {
  const w = lightningAt(windows, ms)
  return w ? `Lightning ${lightningPercent(w)}%` : null
}

/**
 * The band's name for assistive technology; `atText` says when the peak is.
 * `hasGaps` is true when some slots of the chart have no data. A null peak
 * means no slot is covered at all.
 */
export function lightningLabel(peak: LightningWindow | null, atText: string, hasGaps = false): string {
  if (peak === null) return 'No lightning data'
  if (lightningStep(peak.probability) === 0) {
    return hasGaps
      ? 'Lightning probability, below 10% where there is data, no data for the rest'
      : 'Lightning probability, below 10% throughout'
  }
  const highest = `Lightning probability, highest ${lightningPercent(peak)}% at ${atText}`
  return hasGaps ? `${highest}, no data for part of it` : highest
}
