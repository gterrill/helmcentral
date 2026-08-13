import { useEffect, useState } from 'react'

export interface SignalKPath {
  path: string
  units?: string
  value?: number
}

/**
 * Every path the SignalK server is currently publishing, for the gauge path
 * picker. Fetched on demand rather than streamed: it only matters while the
 * config dialog is open.
 */
export function useSignalKPaths(enabled: boolean) {
  const [paths, setPaths] = useState<SignalKPath[]>([])

  useEffect(() => {
    if (!enabled) return
    let cancelled = false

    void (async () => {
      try {
        const response = await fetch('/api/signalk/paths')
        if (!response.ok) return
        const payload = (await response.json()) as { paths?: SignalKPath[] }
        if (!cancelled) setPaths(Array.isArray(payload.paths) ? payload.paths : [])
      } catch {
        // The picker falls back to free text, so a failure here is survivable.
      }
    })()

    return () => { cancelled = true }
  }, [enabled])

  return { paths }
}
