import { useEffect, useState } from 'react'

export interface WeatherProviderInfo {
  id: string
  name: string
  description: string
}

export function useWeatherProviders() {
  const [providers, setProviders] = useState<WeatherProviderInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchProviders = async () => {
      try {
        const response = await fetch('/api/weather-providers')
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`)
        }

        const data = await response.json()
        setProviders(Array.isArray(data) ? data : [])
      } catch (error) {
        console.error('Error fetching weather providers:', error)
        setError(error instanceof Error ? error.message : String(error))
      } finally {
        setLoading(false)
      }
    }

    void fetchProviders()
  }, [])

  return { providers, loading, error }
}
