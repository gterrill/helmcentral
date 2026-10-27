import { declareQuery, useApiQuery } from '@/lib/api-query'

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

// A body without a points array is dropped, as before.
function toTrend(data: DepthTrendApiResponse): DepthTrendData | null {
  if (!Array.isArray(data.points)) return null
  return {
    points: data.points,
    since: data.since === 'tide' ? 'tide' : 'window',
    tideType: data.tide_type === 'high' || data.tide_type === 'low' ? data.tide_type : undefined,
    tideDepthM: data.tide_depth_m,
  }
}

declareQuery(['/api/depth-trend'], { structuralSharing: true })

export function useDepthTrend(queryWindow = '3h', refreshInterval = 60): DepthTrendData {
  // Failures are silent: the last good trend stays on screen.
  const { data } = useApiQuery<DepthTrendData | null>({
    queryKey: ['/api/depth-trend', queryWindow],
    path: `/api/depth-trend?window=${encodeURIComponent(queryWindow)}`,
    refetchInterval: refreshInterval * 1000,
    select: toTrend,
  })
  return data ?? EMPTY_TREND
}
