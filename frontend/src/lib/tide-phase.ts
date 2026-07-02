import type { TideExtremePoint } from '@/hooks/use-tide-chart'

export type TidePhase = 'spring' | 'neap' | null

const SPRING_THRESHOLD = 0.8
const NEAP_THRESHOLD = 0.2

// Classifies today's tidal range against the min/max range seen across the
// full fetched extremes window (not just the visible chart range) - a window
// of ~7-8 days is more than half the ~7.4-day spring-to-neap interval, so it
// reliably contains a local high or low to compare against even without a
// full lunar cycle of data.
export function classifyTidePhase(extremes: TideExtremePoint[], now: Date): TidePhase {
  if (extremes.length < 2) return null

  const sorted = [...extremes].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime())

  const pairs = sorted.slice(0, -1).map((point, i) => {
    const next = sorted[i + 1]
    return {
      startMs: new Date(point.time).getTime(),
      endMs: new Date(next.time).getTime(),
      rangeM: Math.abs(next.heightM - point.heightM),
    }
  })

  const nowMs = now.getTime()
  const currentPair = pairs.find((pair) => nowMs >= pair.startMs && nowMs <= pair.endMs)
  if (!currentPair) return null

  const ranges = pairs.map((pair) => pair.rangeM)
  const minRange = Math.min(...ranges)
  const maxRange = Math.max(...ranges)
  if (maxRange === minRange) return null

  const position = (currentPair.rangeM - minRange) / (maxRange - minRange)
  if (position >= SPRING_THRESHOLD) return 'spring'
  if (position <= NEAP_THRESHOLD) return 'neap'
  return null
}
