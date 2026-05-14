import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'

export function SignalKSettingsPanel() {
  const [signalKAddress, setSignalKAddress] = useState('localhost')
  const [signalKPort, setSignalKPort] = useState('3000')
  const [isConnecting, setIsConnecting] = useState(false)
  const [connectError, setConnectError] = useState<string | null>(null)
  const [connectSuccess, setConnectSuccess] = useState<string | null>(null)

  const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? `${window.location.protocol}//${window.location.hostname}:8080`

  useEffect(() => {
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

    void fetchSignalKSettings()
  }, [apiBaseUrl])

  const connectSignalK = async () => {
    setIsConnecting(true)
    setConnectError(null)
    setConnectSuccess(null)

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

      setConnectSuccess('SignalK settings updated')
    } catch (error) {
      setConnectError(error instanceof Error ? error.message : 'Unable to connect to SignalK')
    } finally {
      setIsConnecting(false)
    }
  }

  return (
    <div className="mx-auto max-w-3xl rounded-lg border bg-background/60 p-4">
      <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">SignalK Connection</h3>

      <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-[minmax(0,1fr)_minmax(0,180px)_auto]">
        <label className="flex min-w-0 items-center gap-2 rounded-md border border-primary/40 bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em]">
          <span className="text-muted-foreground">Address</span>
          <input
            className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
            value={signalKAddress}
            onChange={(event) => setSignalKAddress(event.target.value)}
            aria-label="SignalK address"
          />
        </label>

        <label className="flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em]">
          <span className="text-muted-foreground">Port</span>
          <input
            className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
            value={signalKPort}
            onChange={(event) => setSignalKPort(event.target.value)}
            aria-label="SignalK port"
          />
        </label>

        <Button
          variant="outline"
          className="h-10 whitespace-nowrap border-primary/55 px-4 font-display text-xs tracking-[0.14em] text-primary"
          onClick={connectSignalK}
          disabled={isConnecting}
        >
          {isConnecting ? 'Connecting' : 'Connect'}
        </Button>
      </div>

      {connectError && (
        <div className="mt-3 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs uppercase tracking-[0.08em] text-destructive">
          {connectError}
        </div>
      )}
      {connectSuccess && (
        <div className="mt-3 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-xs uppercase tracking-[0.08em] text-emerald-600">
          {connectSuccess}
        </div>
      )}
    </div>
  )
}
