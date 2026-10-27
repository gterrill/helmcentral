import { memo } from 'react'
import { formatCoordinate, formatHeading } from '@/lib/format'
import { Tile } from '@/components/ui/tile'
import { formatDataAge, isStale } from '@/lib/staleness'
import { PLACE_NAME_REFRESH_SECONDS } from '@/config/app-config'
import { usePlaceName } from '@/hooks/use-place-name'
import { useVesselFields } from '@/hooks/use-vessel-state'

export interface PositionTileProps {
  latitude: number | null
  longitude: number | null
  headingTrue: number | null
  gnssValidationState: string | null
  gnssQualityIndicator: number | null
  gnssHdop: number | null
  gnssValidationReason: string | null
  gnssSatellites: number | null
  placeName: string | null
  /**
   * Seconds since the GNSS fix last updated, or null if the upstream source
   * publishes no age for it. `fetchSignalKVesselState` (backend/signalk.go)
   * carries no per-fix timestamp today, so this is always null until a
   * source actually publishes one — see isStale for why null reads as
   * "unknown," not "stale."
   */
  lastUpdateAgeS: number | null
}

export const PositionTile = memo(function PositionTile({
  latitude,
  longitude,
  headingTrue,
  gnssValidationState,
  gnssQualityIndicator,
  gnssHdop,
  gnssValidationReason,
  gnssSatellites,
  placeName,
  lastUpdateAgeS,
}: PositionTileProps) {
  const feedStale = isStale(lastUpdateAgeS)
  const headingLabel = formatHeading(headingTrue)
  const satellitesValueClass = gnssValidationState === 'critical'
    ? 'text-red-600'
    : gnssValidationState === 'degraded'
      ? 'text-amber-600'
      : 'text-gauge-secondary'
  const gnssDiagnosticLabel = [
    `Q${gnssQualityIndicator !== null ? gnssQualityIndicator : '—'}`,
    `HDOP ${gnssHdop !== null ? gnssHdop.toFixed(1) : '—'}`,
    gnssValidationState ?? '—',
    gnssValidationReason ?? '',
  ].filter(Boolean).join(' · ')

  return (
    <Tile title="Position" stale={feedStale} staleLabel={formatDataAge(lastUpdateAgeS)}>
      <div className="mt-2 flex items-start justify-between gap-4">
        <div>
          <p className="font-mono text-sm">{formatCoordinate(latitude, true)}</p>
          <p className="font-mono text-sm">{formatCoordinate(longitude, false)}</p>
        </div>
        <div className="shrink-0 text-right">
          <p className="font-mono text-sm text-muted-foreground">HDG {headingLabel}</p>
          {gnssValidationState === 'degraded' || gnssValidationState === 'critical' ? (
            <p
              className={`mt-1 max-w-[170px] truncate font-mono text-xs ${satellitesValueClass}`}
              title={gnssDiagnosticLabel}
            >
              {gnssDiagnosticLabel}
            </p>
          ) : (
            <p className="mt-1 font-mono text-sm text-muted-foreground">
              SATS <span className={satellitesValueClass}>{gnssSatellites !== null ? gnssSatellites : '—'}</span>
            </p>
          )}
        </div>
      </div>
      <div className="mt-3 truncate rounded-md bg-gauge-secondary/10 px-3 py-2 font-display text-2xl text-gauge-secondary">
        {placeName ?? '—'}
      </div>
    </Tile>
  )
})

const POSITION_FIELDS = [
  'positionLastUpdateAgeS', 'latitude', 'longitude', 'headingTrue',
  'gnssValidationState', 'gnssQualityIndicator', 'gnssHdop', 'gnssValidationReason', 'gnssSatellites',
] as const

/** The Position tile: the fix and its quality, and the place name the server looks up for it. */
export const LivePositionTile = memo(function LivePositionTile() {
  const { positionLastUpdateAgeS, ...fix } = useVesselFields(POSITION_FIELDS)
  const placeName = usePlaceName(fix.latitude !== null && fix.longitude !== null, PLACE_NAME_REFRESH_SECONDS)
  return <PositionTile lastUpdateAgeS={positionLastUpdateAgeS} {...fix} placeName={placeName} />
})
