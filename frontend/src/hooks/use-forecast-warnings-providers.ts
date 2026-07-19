import { useEffect, useState } from 'react'

export interface ForecastWarningsProviderInfo {
  id: string
  name: string
}

export function useForecastWarningsProviders() {
  const [providers, setProviders] = useState<ForecastWarningsProviderInfo[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const fetchProviders = async () => {
      try {
        const response = await fetch('/api/forecast-warnings-providers')
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`)
        }

        const data = await response.json()
        setProviders(Array.isArray(data) ? data : [])
      } catch (error) {
        console.error('Error fetching forecast warnings providers:', error)
      } finally {
        setLoading(false)
      }
    }

    void fetchProviders()
  }, [])

  return { providers, loading }
}
