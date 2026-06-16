import { useEffect, useState } from 'react'

export interface TideProviderInfo {
  id: string
  name: string
}

export function useTideProviders() {
  const [providers, setProviders] = useState<TideProviderInfo[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const fetchProviders = async () => {
      try {
        const response = await fetch('/api/tide-providers')
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`)
        }

        const data = await response.json()
        setProviders(Array.isArray(data) ? data : [])
      } catch (error) {
        console.error('Error fetching tide providers:', error)
      } finally {
        setLoading(false)
      }
    }

    void fetchProviders()
  }, [])

  return { providers, loading }
}
