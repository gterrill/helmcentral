import { useEffect, useState } from 'react'

export interface DepthTrendPoint {
  time: string
  depth_m: number
}

export function useDepthTrend(queryWindow = '2h', refreshInterval = 60) {
  const [points, setPoints] = useState<DepthTrendPoint[]>([])

  useEffect(() => {
    const fetchPoints = async () => {
      try {
        const res = await fetch(`/api/depth-trend?window=${encodeURIComponent(queryWindow)}`)
        if (!res.ok) return
        const data = (await res.json()) as DepthTrendPoint[]
        if (Array.isArray(data)) setPoints(data)
      } catch {
        // leave stale data on error
      }
    }

    void fetchPoints()
    const id = globalThis.setInterval(() => { void fetchPoints() }, refreshInterval * 1000)
    return () => globalThis.clearInterval(id)
  }, [queryWindow, refreshInterval])

  return points
}
