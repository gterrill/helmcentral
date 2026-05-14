import { useEffect, useMemo, useState } from 'react'

interface RadarDrawerProps {
  latitude: number | null
  longitude: number | null
}

const RADAR_ZOOM = 8
const RECENTER_THRESHOLD_NM = 0.5
const OVERLAYS = ['radar', 'wind', 'temp', 'pressure'] as const
type Overlay = (typeof OVERLAYS)[number]

function haversineMeters(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const toRad = (deg: number) => (deg * Math.PI) / 180
  const dLat = toRad(lat2 - lat1)
  const dLon = toRad(lon2 - lon1)
  const a =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLon / 2) ** 2
  return 6371000 * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a))
}

function buildWindyEmbedUrl(latitude: number, longitude: number, overlay: Overlay): string {
  const params = new URLSearchParams({
    lat: latitude.toFixed(5),
    lon: longitude.toFixed(5),
    zoom: String(RADAR_ZOOM),
    level: 'surface',
    overlay,
    menu: 'true',
    message: '',
    marker: '',
    calendar: 'now',
    pressure: 'true',
    type: 'map',
    location: 'coordinates',
    detail: 'true',
    detailLat: latitude.toFixed(5),
    detailLon: longitude.toFixed(5),
    metricWind: 'kt',
    metricTemp: 'default',
    radarRange: '0',
  })

  return `https://embed.windy.com/embed2.html?${params.toString()}`
}

export function RadarDrawer({ latitude, longitude }: RadarDrawerProps) {
  const [overlay, setOverlay] = useState<Overlay>('radar')
  const [mapCenter, setMapCenter] = useState<{ lat: number; lon: number } | null>(null)
  const hasPosition = latitude !== null && longitude !== null

  useEffect(() => {
    if (!hasPosition || latitude === null || longitude === null) {
      return
    }

    setMapCenter((prev) => {
      if (!prev) {
        return { lat: latitude, lon: longitude }
      }

      const distanceMeters = haversineMeters(prev.lat, prev.lon, latitude, longitude)
      const thresholdMeters = RECENTER_THRESHOLD_NM * 1852

      if (distanceMeters >= thresholdMeters) {
        return { lat: latitude, lon: longitude }
      }

      return prev
    })
  }, [hasPosition, latitude, longitude])

  const embedUrl = useMemo(() => {
    if (!hasPosition || !mapCenter) {
      return null
    }

    return buildWindyEmbedUrl(mapCenter.lat, mapCenter.lon, overlay)
  }, [hasPosition, mapCenter, overlay])

  if (!hasPosition) {
    return (
      <div className="rounded-lg border bg-background/60 px-4 py-8 text-center">
        <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">Radar</p>
        <p className="mt-2 font-medium text-foreground">Waiting for vessel position...</p>
        <p className="mt-1 text-xs text-muted-foreground">The map will center automatically once GPS latitude/longitude are available.</p>
      </div>
    )
  }

  return (
    <div className="space-y-3 pb-4">
      <div className="flex flex-wrap items-center gap-3 rounded-lg border bg-background/60 px-3 py-2 text-xs">
        <label className="inline-flex items-center gap-2 text-muted-foreground">
          Layer
          <select
            value={overlay}
            onChange={(e) => setOverlay(e.target.value as Overlay)}
            className="rounded border bg-background px-2 py-1 text-foreground"
          >
            {OVERLAYS.map((value) => (
              <option key={value} value={value}>{value}</option>
            ))}
          </select>
        </label>

      </div>

      <div className="rounded-lg border bg-background/60 p-2">
        <iframe
          title="Windy Weather Map"
          src={embedUrl ?? undefined}
          className="h-[60vh] min-h-[360px] w-full overflow-hidden rounded-md border-0"
          loading="lazy"
          referrerPolicy="no-referrer-when-downgrade"
          allowFullScreen
        />
      </div>
      <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
        <span>Center: {latitude?.toFixed(5)}, {longitude?.toFixed(5)}</span>
        <a
          href={embedUrl ?? '#'}
          target="_blank"
          rel="noreferrer"
          className="rounded border px-2 py-1 hover:bg-muted"
        >
          Open in Windy
        </a>
      </div>
      <p className="text-xs text-muted-foreground">
        Auto recenter threshold: {RECENTER_THRESHOLD_NM.toFixed(1)} nm
      </p>
    </div>
  )
}
