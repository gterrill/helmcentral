import { useCallback, useEffect, useRef, useState } from 'react'
import { apiRequest } from '@/lib/api-query'

export interface PrefetchBounds {
  west: number
  south: number
  east: number
  north: number
}

export interface PrefetchProgress {
  done: number
  total: number
}

interface PrefetchStartResponse {
  jobId?: string
  totalTiles?: number
}

interface PrefetchStatusResponse {
  total?: number
  done?: number
  complete?: boolean
}

const POLL_INTERVAL_MS = 1000

/**
 * Drives the "cache this area" prefetch flow: POSTs a bbox/zoom-range
 * prefetch request, then polls the job's status endpoint every ~1s until
 * it completes.
 */
export function useImageryPrefetch() {
  const [prefetching, setPrefetching] = useState(false)
  const [progress, setProgress] = useState<PrefetchProgress | null>(null)
  const [error, setError] = useState<string | null>(null)
  const pollTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const clearPoll = useCallback(() => {
    if (pollTimerRef.current !== null) {
      clearInterval(pollTimerRef.current)
      pollTimerRef.current = null
    }
  }, [])

  // Stop polling if the component unmounts mid-job.
  useEffect(() => clearPoll, [clearPoll])

  const startPrefetch = useCallback(
    async (bounds: PrefetchBounds, minZoom: number, maxZoom: number): Promise<string | null> => {
      setError(null)
      setProgress(null)

      let jobId: string
      let total: number

      // The server's own words reach the operator: a 400 carries "area too
      // large: N tiles (max 8000)" in its error field.
      try {
        const body = await apiRequest<PrefetchStartResponse>('/api/world-imagery/prefetch', {
          method: 'POST',
          json: {
            west: bounds.west,
            south: bounds.south,
            east: bounds.east,
            north: bounds.north,
            minZoom,
            maxZoom,
          },
        })
        if (!body.jobId) {
          setError('Prefetch response missing jobId')
          return null
        }
        jobId = body.jobId
        total = body.totalTiles ?? 0

        setPrefetching(true)
        setProgress({ done: 0, total })
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to start prefetch')
        return null
      }

      clearPoll()
      pollTimerRef.current = setInterval(() => {
        void (async () => {
          try {
            const status = await apiRequest<PrefetchStatusResponse>(`/api/world-imagery/prefetch/${jobId}`)
            setProgress({ done: status.done ?? 0, total: status.total ?? total })
            if (status.complete) {
              clearPoll()
              setPrefetching(false)
            }
          } catch (err) {
            clearPoll()
            setPrefetching(false)
            setError(err instanceof Error ? err.message : 'Failed to poll prefetch status')
          }
        })()
      }, POLL_INTERVAL_MS)

      return jobId
    },
    [clearPoll],
  )

  return { prefetching, progress, error, startPrefetch }
}
