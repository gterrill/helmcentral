import { useCallback, useEffect, useRef, useState } from 'react'

export interface ForecastWarningSection {
  day: string
  warningType: string
}

export interface ForecastWarningBulletin {
  id: string
  title: string
  issuedAt: string | null
  sections: ForecastWarningSection[]
  detailsUrl: string
  category: string
}

export interface ForecastWarnings {
  provider: string
  region: string
  bulletins: ForecastWarningBulletin[]
}

// Shared by WindWarningNotice and the Forecast tab indicator so both agree
// on what counts as an active wind warning without duplicating the check.
export function findActiveWindBulletin(warnings: ForecastWarnings | null): ForecastWarningBulletin | undefined {
  if (!warnings) return undefined
  return warnings.bulletins.find((bulletin) => bulletin.category === 'wind' && bulletin.sections.length > 0)
}

interface ForecastWarningsSectionApi {
  day?: string
  warning_type?: string
}

interface ForecastWarningsBulletinApi {
  id?: string
  title?: string
  issued_at?: string
  sections?: ForecastWarningsSectionApi[]
  details_url?: string
  category?: string
}

interface ForecastWarningsApi {
  provider?: string
  region?: string
  bulletins?: ForecastWarningsBulletinApi[]
  cached?: boolean
  updated_at?: string
  ttl_seconds?: number
}

export function useForecastWarnings(refreshIntervalSeconds = 3600) {
  const [warnings, setWarnings] = useState<ForecastWarnings | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [isCached, setIsCached] = useState(false)
  const [updatedAt, setUpdatedAt] = useState<string | null>(null)
  const [ttlSeconds, setTtlSeconds] = useState<number | null>(null)
  const hasLoadedDataRef = useRef(false)

  const fetchWarnings = useCallback(async () => {
    try {
      if (!hasLoadedDataRef.current) {
        setLoading(true)
      }

      const response = await fetch('/api/forecast-warnings')
      if (!response.ok) {
        const errorPayload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(errorPayload?.error ?? `HTTP error! status: ${response.status}`)
      }

      const data = (await response.json()) as ForecastWarningsApi

      // The plugin providing these warnings already scopes bulletins/sections
      // to the vessel's own region and filters out cancellations - every
      // bulletin/section returned here is active and relevant, so there's no
      // client-side filtering left to do.
      const mapped: ForecastWarnings = {
        provider: data.provider ?? '',
        region: data.region ?? '',
        bulletins: Array.isArray(data.bulletins)
          ? data.bulletins.map((bulletin) => ({
              id: bulletin.id ?? '',
              title: bulletin.title ?? '',
              issuedAt: typeof bulletin.issued_at === 'string' && bulletin.issued_at !== '' ? bulletin.issued_at : null,
              detailsUrl: bulletin.details_url ?? '',
              category: bulletin.category ?? '',
              sections: Array.isArray(bulletin.sections)
                ? bulletin.sections.map((section) => ({
                    day: section.day ?? '',
                    warningType: section.warning_type ?? '',
                  }))
                : [],
            }))
          : [],
      }

      hasLoadedDataRef.current = true
      setWarnings(mapped)
      setIsCached(Boolean(data.cached))
      setUpdatedAt(typeof data.updated_at === 'string' ? data.updated_at : null)
      setTtlSeconds(typeof data.ttl_seconds === 'number' ? data.ttl_seconds : null)
      setError(null)
    } catch (err) {
      console.error('Error fetching forecast warnings:', err)
      setError(err instanceof Error ? err.message : 'Failed to load forecast warnings')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchWarnings()
    const interval = setInterval(fetchWarnings, refreshIntervalSeconds * 1000)
    return () => clearInterval(interval)
  }, [fetchWarnings, refreshIntervalSeconds])

  const activeWarning = warnings && warnings.bulletins.length > 0 ? warnings : null

  return { warnings, activeWarning, loading, error, isCached, updatedAt, ttlSeconds, refetch: fetchWarnings }
}
