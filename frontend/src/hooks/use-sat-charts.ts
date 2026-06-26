import { useCallback, useEffect, useState } from 'react'

export interface SatChart {
  id: string
  name: string
  bounds: [number, number, number, number] // [west, south, east, north]
  minzoom: number
  maxzoom: number
  format: string
  size_bytes: number
}

interface SatChartsListResponse {
  charts?: SatChart[]
}

export function useSatCharts() {
  const [charts, setCharts] = useState<SatChart[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchCharts = useCallback(async () => {
    try {
      setLoading(true)
      const res = await fetch('/api/sat-charts')
      if (!res.ok) {
        throw new Error(`HTTP error! status: ${res.status}`)
      }
      const data = (await res.json()) as SatChartsListResponse
      setCharts(Array.isArray(data.charts) ? data.charts : [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load charts')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchCharts()
  }, [fetchCharts])

  const uploadChart = useCallback(async (file: File): Promise<SatChart | null> => {
    const formData = new FormData()
    formData.append('file', file)
    const res = await fetch('/api/sat-charts', { method: 'POST', body: formData })
    if (!res.ok) return null
    const chart = (await res.json()) as SatChart
    await fetchCharts()
    return chart
  }, [fetchCharts])

  const deleteChart = useCallback(async (id: string): Promise<boolean> => {
    const res = await fetch(`/api/sat-charts/${id}`, { method: 'DELETE' })
    if (!res.ok) return false
    await fetchCharts()
    return true
  }, [fetchCharts])

  return { charts, loading, error, refetch: fetchCharts, uploadChart, deleteChart }
}
