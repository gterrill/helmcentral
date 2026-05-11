import { useEffect, useState } from 'react'

interface VesselState {
  status: string
  datetime: string
  depth: number
  source: string
}

export function useVesselState(refreshInterval: number) {
  const [depth, setDepth] = useState<number | null>(null)

  useEffect(() => {
    const fetchVesselState = async () => {
      try {
        const response = await fetch('/api/vessel-state')

        if (!response.ok) {
          throw new Error('Failed to fetch vessel state')
        }

        const data = (await response.json()) as VesselState

        if (typeof data.depth === 'number' && data.depth >= 0) {
          setDepth(data.depth)
        }
      } catch (err) {
        console.error('Failed to fetch vessel state:', err)
      }
    }

    void fetchVesselState()
    const timer = window.setInterval(() => {
      void fetchVesselState()
    }, refreshInterval * 1000)

    return () => {
      window.clearInterval(timer)
    }
  }, [refreshInterval])

  return { depth }
}
