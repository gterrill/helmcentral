import { useEffect, useMemo, useRef, useState } from 'react'

interface RadarDrawerProps {
  latitude: number | null
  longitude: number | null
}

interface LatLon {
  lat: number
  lon: number
}

const RADAR_ZOOM = 8
const OVERLAYS = ['radar', 'wind', 'temp', 'pressure'] as const
const WIND_UNITS = ['kt', 'm/s', 'mph', 'bft'] as const
const TEMP_UNITS = ['default', 'C', 'F'] as const
type Overlay = (typeof OVERLAYS)[number]
type WindUnit = (typeof WIND_UNITS)[number]
type TempUnit = (typeof TEMP_UNITS)[number]

function buildWindyEmbedUrl(
  latitude: number,
  longitude: number,
  overlay: Overlay,
  menuVisible: boolean,
  markerVisible: boolean,
  windUnit: WindUnit,
  tempUnit: TempUnit,
): string {
  const params = new URLSearchParams({
    lat: latitude.toFixed(5),
    lon: longitude.toFixed(5),
    zoom: String(RADAR_ZOOM),
    level: 'surface',
    overlay,
    menu: menuVisible ? 'true' : '',
    message: '',
    marker: markerVisible ? 'true' : '',
    calendar: 'now',
    pressure: 'true',
    type: 'map',
    location: 'coordinates',
    detail: 'true',
    detailLat: latitude.toFixed(5),
    detailLon: longitude.toFixed(5),
    metricWind: windUnit,
    metricTemp: tempUnit,
    radarRange: '0',
  })

  return `https://embed.windy.com/embed2.html?${params.toString()}`
}

export function RadarDrawer({ latitude, longitude }: RadarDrawerProps) {
  const [overlay, setOverlay] = useState<Overlay>('radar')
  const [menuVisible, setMenuVisible] = useState(true)
  const [markerVisible, setMarkerVisible] = useState(false)
  const [windUnit, setWindUnit] = useState<WindUnit>('kt')
  const [tempUnit, setTempUnit] = useState<TempUnit>('default')
  const [mapCenter, setMapCenter] = useState<LatLon | null>(null)
  const [lastKnownPosition, setLastKnownPosition] = useState<LatLon | null>(null)
  const [iframeSrc, setIframeSrc] = useState<string | null>(null)
  const iframeRef = useRef<HTMLIFrameElement | null>(null)
  const hasLivePosition = latitude !== null && longitude !== null

  useEffect(() => {
    if (!import.meta.env.DEV || !iframeRef.current) {
      return
    }

    const iframeEl = iframeRef.current
    const shouldBreak = (window as Window & { __RADAR_DEBUG_BREAKPOINT__?: boolean }).__RADAR_DEBUG_BREAKPOINT__ === true

    const srcDescriptor = Object.getOwnPropertyDescriptor(HTMLIFrameElement.prototype, 'src')
    const originalSetAttribute = Element.prototype.setAttribute

    const traceMutation = (kind: string, value: string) => {
      const traceError = new Error(`[Radar Debug] iframe src mutated via ${kind}`)
      // eslint-disable-next-line no-console
      console.warn('[Radar Debug] iframe src mutation', { kind, value, stack: traceError.stack })
      if (shouldBreak) {
        // eslint-disable-next-line no-debugger
        debugger
      }
    }

    if (srcDescriptor?.set) {
      Object.defineProperty(HTMLIFrameElement.prototype, 'src', {
        configurable: true,
        enumerable: srcDescriptor.enumerable ?? true,
        get: srcDescriptor.get,
        set(value: string) {
          if (this === iframeEl) {
            traceMutation('property:setter', String(value))
          }
          srcDescriptor.set?.call(this, value)
        },
      })
    }

    Element.prototype.setAttribute = function patchedSetAttribute(name: string, value: string) {
      if (this === iframeEl && name.toLowerCase() === 'src') {
        traceMutation('setAttribute', String(value))
      }
      return originalSetAttribute.call(this, name, value)
    }

    return () => {
      Element.prototype.setAttribute = originalSetAttribute
      if (srcDescriptor) {
        Object.defineProperty(HTMLIFrameElement.prototype, 'src', srcDescriptor)
      }
    }
  }, [iframeSrc])

  useEffect(() => {
    if (!hasLivePosition || latitude === null || longitude === null) {
      return
    }

    setLastKnownPosition({ lat: latitude, lon: longitude })

    // Lock initial center to avoid periodic iframe reloads on GPS polling updates.
    setMapCenter((prev) => prev ?? { lat: latitude, lon: longitude })
  }, [hasLivePosition, latitude, longitude])

  const draftEmbedUrl = useMemo(() => {
    if (!mapCenter) {
      return null
    }

    return buildWindyEmbedUrl(mapCenter.lat, mapCenter.lon, overlay, menuVisible, markerVisible, windUnit, tempUnit)
  }, [mapCenter, markerVisible, menuVisible, overlay, tempUnit, windUnit])

  useEffect(() => {
    if (iframeSrc !== null || draftEmbedUrl === null) {
      return
    }
    setIframeSrc(draftEmbedUrl)
  }, [draftEmbedUrl, iframeSrc])

  const handleApply = () => {
    if (!draftEmbedUrl) {
      return
    }
    setIframeSrc(draftEmbedUrl)
  }

  const handleRecenter = () => {
    if (!lastKnownPosition) {
      return
    }
    const nextCenter = { lat: lastKnownPosition.lat, lon: lastKnownPosition.lon }
    setMapCenter(nextCenter)
    setIframeSrc(buildWindyEmbedUrl(nextCenter.lat, nextCenter.lon, overlay, menuVisible, markerVisible, windUnit, tempUnit))
  }

  if (!lastKnownPosition) {
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

        <label className="inline-flex items-center gap-2 text-muted-foreground">
          Wind Unit
          <select
            value={windUnit}
            onChange={(e) => setWindUnit(e.target.value as WindUnit)}
            className="rounded border bg-background px-2 py-1 text-foreground"
          >
            {WIND_UNITS.map((value) => (
              <option key={value} value={value}>{value}</option>
            ))}
          </select>
        </label>

        <label className="inline-flex items-center gap-2 text-muted-foreground">
          Temp Unit
          <select
            value={tempUnit}
            onChange={(e) => setTempUnit(e.target.value as TempUnit)}
            className="rounded border bg-background px-2 py-1 text-foreground"
          >
            {TEMP_UNITS.map((value) => (
              <option key={value} value={value}>{value}</option>
            ))}
          </select>
        </label>

        <label className="inline-flex items-center gap-2 text-muted-foreground">
          <input
            type="checkbox"
            checked={menuVisible}
            onChange={(e) => setMenuVisible(e.target.checked)}
            className="h-4 w-4"
          />
          Menu
        </label>

        <label className="inline-flex items-center gap-2 text-muted-foreground">
          <input
            type="checkbox"
            checked={markerVisible}
            onChange={(e) => setMarkerVisible(e.target.checked)}
            className="h-4 w-4"
          />
          Marker
        </label>

        <button
          type="button"
          onClick={handleApply}
          className="rounded border px-2 py-1 text-foreground hover:bg-muted"
        >
          Apply
        </button>

        <button
          type="button"
          onClick={handleRecenter}
          className="rounded border px-2 py-1 text-foreground hover:bg-muted"
        >
          Recenter
        </button>

      </div>

      <div className="rounded-lg border bg-background/60 p-2">
        <iframe
          ref={iframeRef}
          title="Windy Weather Map"
          src={iframeSrc ?? undefined}
          className="h-[60vh] min-h-[360px] w-full overflow-hidden rounded-md border-0"
          loading="lazy"
          referrerPolicy="no-referrer-when-downgrade"
          allowFullScreen
        />
      </div>
      <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
        <span>
          Center: {mapCenter?.lat.toFixed(5)}, {mapCenter?.lon.toFixed(5)}
        </span>
        <a
          href={iframeSrc ?? '#'}
          target="_blank"
          rel="noreferrer"
          className="rounded border px-2 py-1 hover:bg-muted"
        >
          Open in Windy
        </a>
      </div>
      {!hasLivePosition && (
        <p className="text-xs text-amber-700">Live GPS temporarily unavailable, using last known position.</p>
      )}
      <p className="text-xs text-muted-foreground">Live GPS updates no longer auto-reload the map. Use Recenter when needed.</p>
    </div>
  )
}
