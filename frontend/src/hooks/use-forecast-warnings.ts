import { useEffect, useMemo } from 'react'

import { FORECAST_SURF_WARNING_PATH, FORECAST_WIND_WARNING_PATH } from '@/lib/alarm-display'
import { useApiQuery, useApiQuerySelector } from '@/lib/api-query'
import { forecastQuery, type ForecastQueryOptions } from '@/lib/forecast-place'

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

// Shared by ForecastWarningNotice and the Forecast tab indicator so both
// agree on what counts as an active wind warning without duplicating the
// check.
export function findActiveWindBulletin(warnings: ForecastWarnings | null): ForecastWarningBulletin | undefined {
  if (!warnings) return undefined
  return warnings.bulletins.find((bulletin) => bulletin.category === 'wind' && bulletin.sections.length > 0)
}

// The surf counterpart to findActiveWindBulletin (ADR 0087 adds the surf
// warning path alongside the existing wind one). Kept as a separate
// function rather than a category parameter because the two are called
// from different, unrelated call sites and a shared "kind" argument would
// buy nothing but an extra string to get wrong at each call site.
export function findActiveSurfBulletin(warnings: ForecastWarnings | null): ForecastWarningBulletin | undefined {
  if (!warnings) return undefined
  return warnings.bulletins.find((bulletin) => bulletin.category === 'surf' && bulletin.sections.length > 0)
}

/**
 * The active bulletin's own link for a forecast alarm on `path`, or null
 * when there is none to show: a path that is not one of the two forecast
 * derived paths, no active bulletin for that path's category, or a
 * bulletin whose details_url the provider left empty. The alarm itself
 * carries only the path and a ranked value (ADR 0087), not a URL, so the
 * banner and the drawer both join the two here rather than the alarm
 * carrying a link that would go stale the moment the bulletin does.
 */
export function forecastWarningDetailsUrl(warnings: ForecastWarnings | null, path: string): string | null {
  let bulletin: ForecastWarningBulletin | undefined
  if (path === FORECAST_WIND_WARNING_PATH) bulletin = findActiveWindBulletin(warnings)
  else if (path === FORECAST_SURF_WARNING_PATH) bulletin = findActiveSurfBulletin(warnings)
  else return null

  return bulletin && bulletin.detailsUrl !== '' ? bulletin.detailsUrl : null
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

interface MappedWarnings {
  warnings: ForecastWarnings
  isCached: boolean
  updatedAt: string | null
  ttlSeconds: number | null
}

function mapWarnings(data: ForecastWarningsApi): MappedWarnings {
  // The plugin providing these warnings already scopes bulletins/sections
  // to the vessel's own region and filters out cancellations - every
  // bulletin/section returned here is active and relevant, so there's no
  // client-side filtering left to do.
  const warnings: ForecastWarnings = {
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
  return {
    warnings,
    isCached: Boolean(data.cached),
    updatedAt: typeof data.updated_at === 'string' ? data.updated_at : null,
    ttlSeconds: typeof data.ttl_seconds === 'number' ? data.ttl_seconds : null,
  }
}

export function useForecastWarnings(refreshIntervalSeconds = 3600, options: ForecastQueryOptions = {}) {
  const { data, error, isLoading, refetch } = useApiQuery<MappedWarnings>({
    ...forecastQuery('/api/forecast-warnings', options.place),
    refetchInterval: refreshIntervalSeconds * 1000,
    enabled: options.enabled,
    select: mapWarnings,
    structuralSharing: true,
  })

  useEffect(() => {
    if (error) console.error('Error fetching forecast warnings:', error)
  }, [error])

  // One object while the answer, the error and the loading state hold, so a
  // reader that memoizes on it (the Forecast page's props) holds too.
  return useMemo(() => {
    const warnings = data?.warnings ?? null
    return {
      warnings,
      activeWarning: warnings && warnings.bulletins.length > 0 ? warnings : null,
      loading: isLoading,
      error: error ? error.message || 'Failed to load forecast warnings' : null,
      isCached: data?.isCached ?? false,
      updatedAt: data?.updatedAt ?? null,
      ttlSeconds: data?.ttlSeconds ?? null,
      refetch,
    }
  }, [data, error, isLoading, refetch])
}

// A failed poll keeps the last good warnings beside its error, and the banner
// keeps showing them through useForecastWarnings, so the dot does too: the two
// agree. Warnings the client cannot read are no bulletin, with no dot.
function selectHasWindBulletin(data: ForecastWarningsApi | undefined): boolean {
  if (data === undefined) return false
  return Boolean(findActiveWindBulletin(mapWarnings(data).warnings))
}

/**
 * Whether a wind bulletin is in force, for the sidebar's Forecast dot (ADR
 * 0215). It shares useForecastWarnings' query and re-renders its reader only
 * when the answer to that one question changes.
 */
export function useHasActiveWindBulletin(refreshIntervalSeconds = 3600): boolean {
  return useApiQuerySelector(
    { ...forecastQuery('/api/forecast-warnings', undefined), refetchInterval: refreshIntervalSeconds * 1000 },
    selectHasWindBulletin,
  )
}
