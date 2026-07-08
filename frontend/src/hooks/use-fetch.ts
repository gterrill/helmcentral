import { useEffect, useRef, useState } from 'react'

export interface UseFetchOptions<T> {
  refreshIntervalSeconds: number
  onError?: (error: Error) => void
  validator?: (data: unknown) => data is T
}

export interface UseFetchResult<T> {
  data: T
  loading: boolean
  error: Error | null
}

/**
 * Generic fetch hook that handles loading state, error handling, and polling.
 * Eliminates repetitive fetch code across multiple data-fetching hooks.
 */
export function useFetch<T>(
  endpoint: string,
  defaultValue: T,
  options: UseFetchOptions<T>,
): UseFetchResult<T> {
  const [data, setData] = useState<T>(defaultValue)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  // Latest-ref idiom: callers typically pass an inline `options` object
  // literal (a new identity every render). Reading through this ref instead
  // of closing over `options` directly lets the effect below stay keyed only
  // on `endpoint`/`refreshIntervalSeconds`, so it doesn't re-fetch and reset
  // the polling interval on every render while still calling the latest
  // validator/onError.
  const optionsRef = useRef(options)
  optionsRef.current = options

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        setError(null)
        const response = await fetch(endpoint)
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`)
        }
        const result = await response.json()

        // Use provided validator or just check if result is truthy
        if (optionsRef.current.validator) {
          if (optionsRef.current.validator(result)) {
            setData(result)
          } else {
            throw new Error('Invalid response data')
          }
        } else {
          setData(result)
        }
      } catch (err) {
        const error = err instanceof Error ? err : new Error(String(err))
        setError(error)
        if (optionsRef.current.onError) {
          optionsRef.current.onError(error)
        } else {
          console.error(`Error fetching from ${endpoint}:`, error)
        }
      } finally {
        setLoading(false)
      }
    }

    void fetchData()
    const timer = window.setInterval(() => {
      void fetchData()
    }, options.refreshIntervalSeconds * 1000)

    return () => {
      window.clearInterval(timer)
    }
  }, [endpoint, options.refreshIntervalSeconds])

  return { data, loading, error }
}
