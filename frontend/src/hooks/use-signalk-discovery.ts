import { useCallback, useState } from 'react'
import { apiBaseUrl } from '@/config/api'

export interface DiscoveredServer {
  address: string
  port: number
  url: string
  vessel_name: string
  version: string
}

export interface UseSignalKDiscoveryResult {
  servers: DiscoveredServer[] | null
  scannedSubnet: string | null
  scanning: boolean
  error: string | null
  /**
   * @param hintOverride an address whose /24 should be searched — used by
   * Settings, where the operator has typed the network they mean into the
   * address field. Omitted elsewhere, where the browser's own hostname is the
   * best available guess.
   */
  discover: (hintOverride?: string) => Promise<DiscoveredServer[]>
  reset: () => void
}

/**
 * Asks the backend to sweep the local network for SignalK servers.
 *
 * The backend runs in a container that cannot see the LAN it needs to search,
 * so it needs to be told which network to look at. `window.location.hostname`
 * is the one piece of that the browser reliably knows — on a real install the
 * operator has browsed to the boat computer's LAN address. It is sent only
 * when it is an IPv4 literal; "localhost" or a hostname tells the backend
 * nothing, and it falls back to the currently-configured SignalK address
 * (which is what makes discovery work when a known server has moved).
 */
export function useSignalKDiscovery(): UseSignalKDiscoveryResult {
  const [servers, setServers] = useState<DiscoveredServer[] | null>(null)
  const [scannedSubnet, setScannedSubnet] = useState<string | null>(null)
  const [scanning, setScanning] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const discover = useCallback(async (hintOverride?: string) => {
    setScanning(true)
    setError(null)

    const isIPv4 = (value: string) => /^\d{1,3}(\.\d{1,3}){3}$/.test(value)
    const typed = hintOverride?.trim() ?? ''
    const hostname = window.location.hostname
    // An explicitly-supplied address wins: the operator typed it. Otherwise
    // fall back to how the dashboard was reached.
    const hint = isIPv4(typed) ? typed : isIPv4(hostname) ? hostname : ''

    try {
      const response = await fetch(`${apiBaseUrl}/api/signalk/discover`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hint }),
      })

      if (!response.ok) {
        const errorPayload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(errorPayload?.error ?? 'Unable to search for SignalK servers')
      }

      const data = (await response.json()) as { servers?: DiscoveredServer[]; scanned_subnet?: string }
      const found = data.servers ?? []
      setServers(found)
      setScannedSubnet(data.scanned_subnet ?? null)
      return found
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Unable to search for SignalK servers'
      setError(message)
      setServers([])
      throw err
    } finally {
      setScanning(false)
    }
  }, [])

  const reset = useCallback(() => {
    setServers(null)
    setScannedSubnet(null)
    setError(null)
  }, [])

  return { servers, scannedSubnet, scanning, error, discover, reset }
}
