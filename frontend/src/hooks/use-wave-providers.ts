import { useEffect, useState } from 'react'

export interface WaveProviderInfo {
  id: string
  name: string
  description: string
}

export function useWaveProviders() {
  const [providers, setProviders] = useState<WaveProviderInfo[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const fetchProviders = async () => {
      try {
        const response = await fetch('/api/wave-providers')
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`)
        }

        const data = await response.json()
        setProviders(Array.isArray(data) ? data : [])
      } catch (error) {
        console.error('Error fetching wave providers:', error)
      } finally {
        setLoading(false)
      }
    }

    void fetchProviders()
  }, [])

  return { providers, loading }
}
