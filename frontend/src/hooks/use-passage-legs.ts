import { useMemo } from 'react'

import { useReplaySigns } from '@/hooks/use-replay-signs'
import { passageLegs, type LegPassage, type PassageLeg } from '@/lib/passage-legs'

const NONE: PassageLeg[] = []

/** The legs of the passages shown, in the order given: one colour per leg. It
 * reads the signs over their span, the same read the replay makes, so the
 * legend and the map agree. The signs read are returned too, for callers that
 * place them as well. */
export function usePassageLegs(passages: readonly { id: string; start: string; end: string }[]) {
  // Keyed by value: callers hand over a fresh array each render, and the map
  // redraws its lines whenever the legs change.
  const key = JSON.stringify(passages.map((p) => [p.id, p.start, p.end]))
  const ranges = useMemo<LegPassage[]>(
    () => (JSON.parse(key) as [string, string, string][]).map(([id, start, end]) => ({ id, startMs: Date.parse(start), endMs: Date.parse(end) })),
    [key],
  )
  const from = ranges.length > 0 ? Math.min(...ranges.map((r) => r.startMs)) : null
  const to = ranges.length > 0 ? Math.max(...ranges.map((r) => r.endMs)) : null
  const { signs, error } = useReplaySigns(from, to)
  const legs = useMemo(() => (ranges.length > 0 ? passageLegs(ranges, signs) : NONE), [ranges, signs])
  return { legs, signs, error, from, to }
}
