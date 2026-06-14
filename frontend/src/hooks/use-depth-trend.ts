import { useEffect, useState } from 'react'

export interface DepthTrendPoint {
  time: string
  depth_m: number
}

export type DepthTrendSince = 'tide' | 'window'
export type TideType = 'high' | 'low'

export interface DepthTrendData {
  points: DepthTrendPoint[]
  since: DepthTrendSince
  tideType?: TideType
  tideDepthM?: number
}

interface DepthTrendApiResponse {
  points: DepthTrendPoint[]
  since?: string
  tide_type?: string
  tide_depth_m?: number
}

const EMPTY_TREND: DepthTrendData = { points: [], since: 'window' }

export function useDepthTrend(queryWindow = '3h', refreshInterval = 60): DepthTrendData {
  const [trend, setTrend] = useState<DepthTrendData>(EMPTY_TREND)

  useEffect(() => {
    const fetchPoints = async () => {
      try {
        const res = await fetch(`/api/depth-trend?window=${encodeURIComponent(queryWindow)}`)
        if (!res.ok) return
        const data = (await res.json()) as DepthTrendApiResponse
        if (!Array.isArray(data.points)) return
        setTrend({
          points: data.points,
          since: data.since === 'tide' ? 'tide' : 'window',
          tideType: data.tide_type === 'high' || data.tide_type === 'low' ? data.tide_type : undefined,
          tideDepthM: data.tide_depth_m,
        })
      } catch {
        // leave stale data on error
      }
    }

    void fetchPoints()
    const id = globalThis.setInterval(() => { void fetchPoints() }, refreshInterval * 1000)
    return () => globalThis.clearInterval(id)
  }, [queryWindow, refreshInterval])

  return trend
}
