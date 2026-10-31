import { useCallback, useState } from 'react'

import { apiRequest, useApiQuery } from '@/lib/api-query'
import { errorMessage } from '@/lib/errors'

/**
 * Outside assistants (backend/mcp_routes.go): the tokens that let a program
 * such as Claude Code read the boat, each token's tool list, and the log of
 * what those programs asked for. Admin tier on the server.
 */
export interface OutsideAssistantToken {
  id: string
  name: string
  created_at: string
  last_used_at: string | null
  /** Tool names this token may use. */
  tools: string[]
}

export interface OutsideAssistantTool {
  name: string
  plain_name: string
  default_on: boolean
}

export interface OutsideAssistantActivity {
  at: string
  client: string
  kind: 'tool' | 'resource' | 'request'
  name: string
  outcome: string
  duration_ms: number
}

interface OutsideAssistantsBody {
  tokens: OutsideAssistantToken[]
  tools: OutsideAssistantTool[]
}

interface ActivityBody {
  activity: OutsideAssistantActivity[]
}

export interface CreatedOutsideAssistantToken {
  token: OutsideAssistantToken
  /** The secret. The server shows it this once. */
  secret: string
}

export const OUTSIDE_ASSISTANTS_PATH = '/api/outside-assistants'
export const OUTSIDE_ASSISTANTS_ACTIVITY_PATH = '/api/outside-assistants/activity'

const NO_TOKENS: OutsideAssistantToken[] = []
const NO_TOOLS: OutsideAssistantTool[] = []
const NO_ACTIVITY: OutsideAssistantActivity[] = []
const ACTIVITY_POLL_MS = 15_000

export function useOutsideAssistants() {
  const list = useApiQuery<OutsideAssistantsBody>({ queryKey: [OUTSIDE_ASSISTANTS_PATH], path: OUTSIDE_ASSISTANTS_PATH })
  const activity = useApiQuery<ActivityBody>({
    queryKey: [OUTSIDE_ASSISTANTS_ACTIVITY_PATH],
    path: OUTSIDE_ASSISTANTS_ACTIVITY_PATH,
    refetchInterval: ACTIVITY_POLL_MS,
  })
  const { refetch: refetchList } = list
  const { refetch: refetchActivity } = activity
  const [refreshError, setRefreshError] = useState<string | null>(null)

  // A change that succeeded is not undone by a list that would not reload. The
  // reload runs after the result is returned, and its failure is shown on its own.
  const refresh = useCallback(() => {
    void refetchList().then(
      () => setRefreshError(null),
      (err: unknown) => setRefreshError(`Could not refresh the token list: ${errorMessage(err)}`),
    )
  }, [refetchList])

  const create = useCallback(
    async (name: string): Promise<CreatedOutsideAssistantToken> => {
      const created = await apiRequest<CreatedOutsideAssistantToken>(`${OUTSIDE_ASSISTANTS_PATH}/tokens`, { method: 'POST', json: { name } })
      refresh()
      return created
    },
    [refresh],
  )

  const setTools = useCallback(
    async (id: string, tools: string[]): Promise<void> => {
      await apiRequest(`${OUTSIDE_ASSISTANTS_PATH}/tokens/${encodeURIComponent(id)}/tools`, { method: 'PUT', json: { tools } })
      refresh()
    },
    [refresh],
  )

  const revoke = useCallback(
    async (id: string): Promise<void> => {
      await apiRequest(`${OUTSIDE_ASSISTANTS_PATH}/tokens/${encodeURIComponent(id)}`, { method: 'DELETE' })
      refresh()
    },
    [refresh],
  )

  return {
    tokens: list.data?.tokens ?? NO_TOKENS,
    tools: list.data?.tools ?? NO_TOOLS,
    activity: activity.data?.activity ?? NO_ACTIVITY,
    loading: list.isLoading,
    error: list.error?.message ?? null,
    activityError: activity.error?.message ?? null,
    refreshError,
    create,
    setTools,
    revoke,
    refreshActivity: refetchActivity,
  }
}
