import { useEffect, useMemo, useState } from 'react'
import { Circle, Sun } from 'lucide-react'

import { appConfig } from '@/config/app-config'
import { Button } from '@/components/ui/button'

function formatClock(date: Date) {
  return new Intl.DateTimeFormat('en-US', {
    hour: 'numeric',
    minute: '2-digit',
    second: '2-digit',
    hour12: true,
  }).format(date)
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
  const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? `${window.location.protocol}//${window.location.hostname}:8080`

  useEffect(() => {
    const fetchVesselState = async () => {
      try {
        const response = await fetch(`${apiBaseUrl}/api/vessel-state`)

        if (!response.ok) {
          throw new Error('Failed to fetch vessel state')
        }

        const data = (await response.json()) as {
          status?: string
          datetime?: string
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

    void fetchVesselState()
    const timer = window.setInterval(() => {
      void fetchVesselState()
    }, 1000)

    return () => window.clearInterval(timer)
  }, [apiBaseUrl])

  const currentDate = useMemo(() => formatDate(now).toUpperCase(), [now])
  const statusText = `${appConfig.boat.model} · ${vesselStatus}`

  return (
    <header className="rounded-xl border bg-card/90 shadow-sm backdrop-blur-sm">
      <div className="grid min-h-20 items-center gap-3 px-4 py-3 md:grid-cols-[max-content_max-content_1fr_auto_auto] md:px-6">
        <div className="flex min-h-10 items-center gap-3 border-border/70 pr-0 md:border-r md:pr-6">
          <p className="font-display text-[2rem] leading-none tracking-[0.16em] text-primary">{appConfig.boat.name}</p>
          <p className="pt-2 text-xs font-medium uppercase tracking-[0.12em] text-muted-foreground">{statusText}</p>
        </div>

        <div className="grid grid-cols-[auto_auto_auto] items-center gap-2 border-border/70 pl-0 md:border-r md:px-6">
          <div className="flex min-w-[170px] items-center gap-2 rounded-md border border-primary/40 bg-background/80 px-2 py-1.5 text-xs uppercase tracking-[0.12em]">
            <span className="text-muted-foreground">Cerbo</span>
            <span className="font-semibold text-foreground/80">192.168.8.157</span>
          </div>
          <div className="flex min-w-[90px] items-center gap-2 rounded-md border border-border bg-background/80 px-2 py-1.5 text-xs uppercase tracking-[0.12em]">
            <span className="text-muted-foreground">Port</span>
            <span className="font-semibold text-foreground/80">9001</span>
          </div>
          <Button variant="outline" size="sm" className="h-9 border-primary/55 px-5 font-display text-[0.72rem] tracking-[0.18em] text-primary">
            Connect
          </Button>
        </div>

        <div className="hidden items-center gap-2 pl-6 md:flex">
          <span className="text-xs font-medium uppercase tracking-[0.12em] text-muted-foreground">VRM:</span>
          <span className="text-xs font-semibold tracking-[0.1em] text-muted-foreground/90">c0619ab58146</span>
        </div>

        <div className="flex items-center gap-2 border-border/70 pl-0 md:border-l md:pl-6">
          <div className="inline-flex items-center gap-1.5 rounded-md border border-border bg-background/70 px-2.5 py-1.5 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            <Circle className="h-2.5 w-2.5 fill-secondary text-secondary" />
            Live
          </div>
          <div className="inline-flex items-center gap-1.5 rounded-md border border-border bg-background/70 px-2.5 py-1.5 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            <Sun className="h-3.5 w-3.5" />
            Day Mode
          </div>
        </div>

        <div className="flex items-center gap-5 justify-self-end">
          <span className="hidden text-sm font-semibold uppercase tracking-[0.14em] text-foreground/85 md:inline">{currentDate}</span>
          <time className="font-display text-5xl leading-none tracking-[0.05em] text-secondary">{formatClock(now)}</time>
        </div>
      </div>
    </header>
  )
}