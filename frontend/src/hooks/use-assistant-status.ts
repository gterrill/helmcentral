import { useApiQuery } from '@/lib/api-query'

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
  const { data, isLoading, error, refetch } = useApiQuery<AssistantStatus>({
    queryKey: ['/api/assistant/status'],
    path: '/api/assistant/status',
  })
  return { status: data ?? null, loading: isLoading, error: error?.message ?? null, refresh: refetch }
}
