import { useEffect, useMemo, useState } from 'react'
import { Circle, Sun } from 'lucide-react'

import { appConfig, uiConfig } from '@/config/app-config'
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
  const [depth, setDepth] = useState<number | null>(null)
  const [signalKAddress, setSignalKAddress] = useState('localhost')
  const [signalKPort, setSignalKPort] = useState('3000')
  const [isConnecting, setIsConnecting] = useState(false)
  const [connectError, setConnectError] = useState<string | null>(null)
  const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? `${window.location.protocol}//${window.location.hostname}:8080`

  useEffect(() => {
    const clockTimer = window.setInterval(() => {
      setNow((current) => new Date(current.getTime() + 1000))
    }, 1000)

    const fetchSignalKSettings = async () => {
      try {
        const response = await fetch(`${apiBaseUrl}/api/settings/signalk`)
        if (!response.ok) {
          throw new Error('Failed to fetch SignalK settings')
        }

        const data = (await response.json()) as {
          address?: string
          port?: number
        }

        if (data.address) {
          setSignalKAddress(data.address)
        }

        if (data.port) {
          setSignalKPort(String(data.port))
        }
      } catch {
        // Keep defaults if settings endpoint is unavailable.
      }
    }

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

        if (typeof data.depth === 'number' && data.depth >= 0) {
          setDepth(data.depth)
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

    void fetchSignalKSettings()
    void fetchVesselState()
    const syncTimer = window.setInterval(() => {
      void fetchVesselState()
    }, uiConfig.vesselStateRefreshSeconds * 1000)

    return () => {
      window.clearInterval(clockTimer)
      window.clearInterval(syncTimer)
    }
  }, [apiBaseUrl])

  const currentDate = useMemo(() => formatDate(now).toUpperCase(), [now])
  const statusText = `${appConfig.boat.model} · ${vesselStatus}`

  const connectSignalK = async () => {
    setIsConnecting(true)
    setConnectError(null)

    const normalizedPort = Number.parseInt(signalKPort, 10)
    const payload = {
      address: signalKAddress.trim(),
      port: Number.isFinite(normalizedPort) ? normalizedPort : 3000,
    }

    try {
      const response = await fetch(`${apiBaseUrl}/api/settings/signalk`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
      })

      if (!response.ok) {
        const errorPayload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(errorPayload?.error ?? 'Unable to connect to SignalK')
      }

      const data = (await response.json()) as {
        address?: string
        port?: number
      }

      if (data.address) {
        setSignalKAddress(data.address)
      }
      if (data.port) {
        setSignalKPort(String(data.port))
      }
    } catch (error) {
      setConnectError(error instanceof Error ? error.message : 'Unable to connect to SignalK')
    } finally {
      setIsConnecting(false)
    }
  }

  return (
    <header className="rounded-xl border bg-card/90 shadow-sm backdrop-blur-sm">
      <div className="flex min-h-16 items-center gap-2 overflow-hidden px-2 py-2 md:px-4">
        <div className="flex min-w-0 flex-[1.3] items-center gap-2 border-border/70 pr-0 md:border-r md:pr-4">
          <p className="min-w-0 shrink-0 font-display text-[1.28rem] leading-none tracking-[0.12em] text-primary md:text-[1.45rem] lg:text-[1.7rem]">
            {appConfig.boat.name}
          </p>
          <p className="min-w-0 truncate pt-1 text-[9px] font-medium uppercase tracking-[0.08em] text-muted-foreground md:text-[10px] lg:text-xs">
            {statusText}
          </p>
        </div>

        <div className="flex min-w-0 flex-[1.35] flex-col gap-1 border-border/70 pl-0 md:border-r md:px-4">
          <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_minmax(0,0.68fr)_auto] items-center gap-1">
            <div className="flex min-w-0 items-center gap-1 rounded-md border border-primary/40 bg-background/80 px-2 py-1 text-[10px] uppercase tracking-[0.1em] md:text-[11px]">
              <span className="text-muted-foreground">SignalK</span>
              <input
                className="min-w-0 w-full bg-transparent font-semibold text-foreground/80 outline-none"
                value={signalKAddress}
                onChange={(event) => setSignalKAddress(event.target.value)}
                aria-label="SignalK address"
              />
            </div>
            <div className="flex min-w-0 items-center gap-1 rounded-md border border-border bg-background/80 px-2 py-1 text-[10px] uppercase tracking-[0.1em] md:text-[11px]">
              <span className="text-muted-foreground">Port</span>
              <input
                className="min-w-0 w-full bg-transparent font-semibold text-foreground/80 outline-none"
                value={signalKPort}
                onChange={(event) => setSignalKPort(event.target.value)}
                aria-label="SignalK port"
              />
            </div>
            <Button
              variant="outline"
              size="sm"
              className="h-8 whitespace-nowrap border-primary/55 px-3 font-display text-[0.63rem] tracking-[0.14em] text-primary md:px-4 md:text-[0.68rem]"
              onClick={connectSignalK}
              disabled={isConnecting}
            >
              {isConnecting ? 'Connecting' : 'Connect'}
            </Button>
          </div>

          {connectError ? (
            <div className="min-h-4 text-[9px] uppercase tracking-[0.08em] text-destructive md:text-[10px]">
              {connectError}
            </div>
          ) : null}
        </div>

        <div className="flex items-center gap-1 border-border/70 pl-0 md:border-l md:pl-4">
          <div className="inline-flex items-center gap-1 rounded-md border border-border bg-background/70 px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground md:text-[11px]">
            <Circle className="h-2.5 w-2.5 fill-secondary text-secondary" />
            Live
          </div>
          <div className="inline-flex items-center gap-1 rounded-md border border-border bg-background/70 px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground md:text-[11px]">
            <Sun className="h-3.5 w-3.5" />
            Day Mode
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-2 justify-self-end md:gap-3">
          <span className="whitespace-nowrap text-[10px] font-semibold uppercase tracking-[0.1em] text-foreground/85 sm:inline md:text-[11px]">
            {currentDate}
          </span>
          {depth !== null && (
            <div className="whitespace-nowrap text-[10px] font-semibold uppercase tracking-[0.1em] text-foreground/85 md:text-[11px]">
              Depth: <span className="font-display text-[12px] font-semibold md:text-[13px]">{depth.toFixed(1)}m</span>
            </div>
          )}
          <time className="whitespace-nowrap text-right font-display text-[1.9rem] leading-none tabular-nums tracking-[0.02em] text-secondary sm:w-[7.5rem] sm:text-[2rem] md:w-[8.2rem] md:text-[2.2rem] lg:w-[9rem] lg:text-[2.6rem]">
            {formatClock(now)}
          </time>
        </div>
      </div>
    </header>
  )
}