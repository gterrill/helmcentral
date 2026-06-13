import { useEffect, useMemo, useState } from 'react'
import { Circle, Sun } from 'lucide-react'

import { uiConfig } from '@/config/app-config'

function formatClock(date: Date) {
  const value = new Intl.DateTimeFormat('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: true,
  }).format(date)

  const [timePart = '--:--:--', meridiem = ''] = value.toUpperCase().split(/\s+/)
  return { timePart, meridiem }
}

function formatDate(date: Date) {
  return new Intl.DateTimeFormat('en-US', {
    weekday: 'long',
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  }).format(date)
}

export function MarineHeader() {
  const [now, setNow] = useState(() => new Date())
  const [vesselStatus, setVesselStatus] = useState('At Anchor')
  const [boatName, setBoatName] = useState<string | null>(null)
  const [boatModel, setBoatModel] = useState<string | null>(null)
  const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? `${window.location.protocol}//${window.location.hostname}:8080`

  useEffect(() => {
    const clockTimer = window.setInterval(() => {
      setNow((current) => new Date(current.getTime() + 1000))
    }, 1000)

    const fetchVesselState = async () => {
      try {
        const response = await fetch(`${apiBaseUrl}/api/vessel-state`)

        if (!response.ok) {
          throw new Error('Failed to fetch vessel state')
        }

        const data = (await response.json()) as {
          status?: string
          datetime?: string
          depth?: number
        }

        if (data.status) {
          setVesselStatus(data.status)
        }

        if (data.datetime) {
          const backendTime = new Date(data.datetime)
          if (!Number.isNaN(backendTime.getTime())) {
            setNow(backendTime)
          }
        }
      } catch {
        // Keep existing values when backend is temporarily unavailable.
      }
    }

    const fetchSettings = async () => {
      try {
        const response = await fetch(`${apiBaseUrl}/api/settings`)

        if (!response.ok) {
          throw new Error('Failed to fetch settings')
        }

        const data = (await response.json()) as {
          boat?: {
            name?: string
            model?: string
          }
        }

        const nextName = data.boat?.name?.trim() ?? ''
        const nextModel = data.boat?.model?.trim() ?? ''

        setBoatName(nextName.length > 0 ? nextName : null)
        setBoatModel(nextModel.length > 0 ? nextModel : null)
      } catch {
        // Show missing settings explicitly instead of falling back to compiled defaults.
        setBoatName(null)
        setBoatModel(null)
      }
    }

    void fetchVesselState()
    void fetchSettings()
    const syncTimer = window.setInterval(() => {
      void fetchVesselState()
      void fetchSettings()
    }, uiConfig.vesselStateRefreshSeconds * 1000)

    return () => {
      window.clearInterval(clockTimer)
      window.clearInterval(syncTimer)
    }
  }, [apiBaseUrl])

  const currentDate = useMemo(() => formatDate(now).toUpperCase(), [now])
  const clock = useMemo(() => formatClock(now), [now])
  const [hh = '--', mm = '--', ss = '--'] = clock.timePart.split(':')
  const vesselNameLabel = boatName ?? 'VESSEL NAME NOT SET'
  const vesselModelLabel = boatModel ?? 'MODEL NOT SET'
  const statusText = `${vesselModelLabel} · ${vesselStatus}`

  return (
    <header className="rounded-xl border bg-card/90 shadow-sm backdrop-blur-sm">
      <div className="flex min-h-16 items-center gap-2 px-2 py-2 md:px-4">
        <div className="flex min-w-0 shrink flex-col border-border/70 pr-0 md:border-r md:pr-4">
          <p className="shrink-0 font-display text-[1.28rem] leading-none tracking-[0.12em] text-primary md:text-[1.45rem] lg:text-[1.7rem]">
            {vesselNameLabel}
          </p>
          <p className="truncate text-[9px] font-medium uppercase tracking-[0.08em] text-muted-foreground md:text-[10px] lg:text-xs">
            {statusText}
          </p>
        </div>

        <div className="ml-auto flex shrink-0 items-center gap-1 border-border/70 pl-0 md:border-l md:pl-4">
          <div className="inline-flex items-center gap-1 rounded-md border border-border bg-background/70 px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground md:text-[11px]">
            <Circle className="h-2.5 w-2.5 fill-secondary text-secondary" />
            Live
          </div>
          <div className="inline-flex items-center gap-1 rounded-md border border-border bg-background/70 px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground md:text-[11px]">
            <Sun className="h-3.5 w-3.5" />
            Day Mode
          </div>
        </div>

        <div className="flex shrink-0 flex-col items-end border-border/70 pl-0 md:border-l md:pl-4">
          <span className="whitespace-nowrap text-[10px] font-semibold uppercase tracking-[0.1em] text-foreground/85 md:text-[11px]">
            {currentDate}
          </span>
          <time className="inline-flex items-baseline whitespace-nowrap font-display leading-[1.08] tracking-[0.02em] text-secondary">
            <span className="inline-block w-[5ch] text-right tabular-nums text-[1.65rem] sm:text-[1.8rem] md:text-[2rem] lg:text-[2.15rem]">
              {hh}:{mm}
            </span>
            <span className="inline-block w-[3ch] text-left tabular-nums text-[1.65rem] sm:text-[1.8rem] md:text-[2rem] lg:text-[2.15rem]">
              :{ss}
            </span>
            <span className="ml-1 text-[1.05rem] tracking-[0.06em] sm:text-[1.15rem] md:text-[1.3rem] lg:text-[1.4rem]">
              {clock.meridiem}
            </span>
          </time>
        </div>
      </div>
    </header>
  )
}