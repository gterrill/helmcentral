import { useCallback, useEffect, useState } from 'react'
import { apiBaseUrl } from '@/config/api'

/**
 * ADR 0093: `problem` is an actionable sentence naming the reason the
 * assistant cannot run right now (disabled, no OpenRouter key, blank
 * model). It is absent once every precondition is met, which is what the
 * drawer checks before it lets the operator type anything.
 */
export interface AssistantStatus {
  enabled: boolean
  configured: boolean
  model: string
  problem?: string
}

export function useAssistantStatus() {
  const [status, setStatus] = useState<AssistantStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const response = await fetch(`${apiBaseUrl}/api/assistant/status`)
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const data = (await response.json()) as AssistantStatus
      setStatus(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void refresh() }, [refresh])

  return { status, loading, error, refresh }
}
