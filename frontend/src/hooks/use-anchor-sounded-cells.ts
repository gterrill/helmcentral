import { useEffect, useRef } from 'react'
import { declareQuery, useApiQuery } from '@/lib/api-query'
import { parseSoundedCells, type SoundedCellsAnswer } from '@/lib/sounded-cells'

declareQuery(['/api/anchor-watch/sounded-cells'], { structuralSharing: true })

export interface SoundedCellsWatch {
  // Whether to ask at all: a watch is set and the map can draw.
  enabled: boolean
  // The watch's identity and anchor position. A new drop or a corrected anchor
  // is a new query, so the cells are asked for again at once.
  setAt: string | null
  lat: number | null
  lon: number | null
}

// The server adds a cell at most once a minute, so it is polled once a minute,
// and only while a watch is set. An answer that cannot be read is logged once
// for each distinct reason and leaves no cells.
export function useAnchorSoundedCells({ enabled, setAt, lat, lon }: SoundedCellsWatch) {
  const { data, error } = useApiQuery<SoundedCellsAnswer>({
    queryKey: ['/api/anchor-watch/sounded-cells', setAt, lat, lon],
    path: '/api/anchor-watch/sounded-cells',
    refetchInterval: 60_000,
    enabled,
    select: parseSoundedCells,
  })
  const logged = useRef<string | null>(null)
  const message = enabled && error ? error.message : null
  useEffect(() => {
    if (message !== null && message !== logged.current) {
      logged.current = message
      console.error('[anchor-watch] sounded cells unreadable:', message)
    }
  }, [message])
  return { answer: enabled ? (data ?? null) : null, error: enabled ? error : null }
}
