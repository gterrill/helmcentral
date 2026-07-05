import { useCallback, useEffect, useRef, useState } from 'react'

export interface MarineWarningSection {
  day: string
  warningType: string
  zones: string[]
}

export interface MarineWarningBulletin {
  productId: string
  title: string
  issuedAtRaw: string
  issuedAt: string | null
  sections: MarineWarningSection[]
  detailsUrl: string
  category: string
}

export interface MarineWarnings {
  state: string
  zone: string
  bulletins: MarineWarningBulletin[]
}

// Shared by WindWarningNotice and the Forecast tab indicator so both agree
// on what counts as an active wind warning without duplicating the check.
export function findActiveWindBulletin(warnings: MarineWarnings | null): MarineWarningBulletin | undefined {
  if (!warnings) return undefined
  return warnings.bulletins.find((bulletin) => bulletin.category === 'wind' && bulletin.sections.length > 0)
}

interface MarineWarningsSectionApi {
  day?: string
  warning_type?: string
  zones?: string[]
}

interface MarineWarningsBulletinApi {
  product_id?: string
  title?: string
  issued_at_raw?: string
  issued_at?: string
  sections?: MarineWarningsSectionApi[]
  details_url?: string
  category?: string
  has_active_warning?: boolean
  has_active_warning_for_zone?: boolean
}

interface MarineWarningsApi {
  state?: string
  zone?: string
  bulletins?: MarineWarningsBulletinApi[]
  has_active_warning?: boolean
  cached?: boolean
  updated_at?: string
  ttl_seconds?: number
}

export function useMarineWarnings(refreshIntervalSeconds = 3600) {
  const [warnings, setWarnings] = useState<MarineWarnings | null>(null)
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

      const response = await fetch('/api/marine-warnings')
      if (!response.ok) {
        const errorPayload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(errorPayload?.error ?? `HTTP error! status: ${response.status}`)
      }

      const data = (await response.json()) as MarineWarningsApi

      // Only surface bulletins/sections that are relevant to the vessel's own
      // zone and currently active - a "Cancellation" or a warning for some
      // other zone in the same state must not be shown here. This mirrors
      // the backend's has_active_warning_for_zone scoping (see backend fix:
      // a Gold Coast surf warning must not alert a vessel near Yeppoon).
      const mapped: MarineWarnings = {
        state: data.state ?? '',
        zone: data.zone ?? '',
        bulletins: Array.isArray(data.bulletins)
          ? data.bulletins
              .filter((bulletin) => bulletin.has_active_warning_for_zone === true)
              .map((bulletin) => ({
                productId: bulletin.product_id ?? '',
                title: bulletin.title ?? '',
                issuedAtRaw: bulletin.issued_at_raw ?? '',
                issuedAt: typeof bulletin.issued_at === 'string' ? bulletin.issued_at : null,
                detailsUrl: bulletin.details_url ?? '',
                category: bulletin.category ?? '',
                sections: Array.isArray(bulletin.sections)
                  ? bulletin.sections
                      .filter(
                        (section) =>
                          (section.warning_type ?? '').toLowerCase() !== 'cancellation' &&
                          Array.isArray(section.zones) &&
                          (data.zone ? section.zones.includes(data.zone) : false),
                      )
                      .map((section) => ({
                        day: section.day ?? '',
                        warningType: section.warning_type ?? '',
                        zones: Array.isArray(section.zones) ? section.zones : [],
                      }))
                  : [],
              }))
              .filter((bulletin) => bulletin.sections.length > 0)
          : [],
      }

      hasLoadedDataRef.current = true
      setWarnings(mapped)
      setIsCached(Boolean(data.cached))
      setUpdatedAt(typeof data.updated_at === 'string' ? data.updated_at : null)
      setTtlSeconds(typeof data.ttl_seconds === 'number' ? data.ttl_seconds : null)
      setError(null)
    } catch (err) {
      console.error('Error fetching marine warnings:', err)
      setError(err instanceof Error ? err.message : 'Failed to load marine warnings')
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
