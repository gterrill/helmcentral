import { WifiOff } from 'lucide-react'
import { useTelemetryStatus } from '@/hooks/use-telemetry-stream'

const BANNER_BASE = 'flex min-w-0 shrink-0 items-start gap-3 rounded-lg border px-4 py-3'
const BANNER_TONES = {
  danger: 'border-destructive bg-destructive/10 text-destructive',
  neutral: 'border-border bg-card',
} as const

/** The container classes the banners above the page share. */
export function bannerClass(tone: keyof typeof BANNER_TONES): string {
  return `${BANNER_BASE} ${BANNER_TONES[tone]}`
}

/** Connectivity is not dismissible: retained values are not live evidence.
 * Lives outside the panel scroller and keeps its text visible on phones. */
export function ConnectionBanner() {
  const status = useTelemetryStatus()
  if (status === 'connected') return null

  return (
    <div role="alert" className={bannerClass('danger')}>
      <WifiOff className="size-5 shrink-0" aria-hidden="true" />
      <div className="min-w-0">
        <p className="text-sm font-semibold">Server connection unavailable</p>
        <p className="text-xs">Telemetry, anchor watch and alarm status may be out of date.</p>
        <p className="mt-1 text-xs font-medium">Reconnecting automatically — check Wi-Fi or mobile data.</p>
      </div>
    </div>
  )
}