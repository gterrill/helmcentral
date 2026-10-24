import { ApiError, apiRequest, invalidateQueries } from '@/lib/api-query'
import { mapProposal, type AssistantProposal, type ProposalApi } from '@/hooks/use-assistant-conversations'
import { todayISO } from '@/lib/local-date'
import { PLANNED_STAY_PATH } from '@/hooks/use-planned-stay'

/**
 * Apply and Dismiss on a maintenance proposal card (ADR 0146). Apply is the
 * one place Mate's proposed changes are written, so it carries the same
 * `?today=` every maintenance write does (the operator's own local date, never
 * the server's).
 */

/** A refused apply or dismiss: an ApiError carrying the server's own reason,
 * verbatim. A 409 means the proposal can no longer be applied as written (a
 * rule changed since Mate proposed it, or it was already dismissed or
 * applied). */
export { ApiError as ProposalActionError } from '@/lib/api-query'

/** On a 409, which stored status the server refused for: 'stale',
 * 'dismissed' or 'applied'. */
export function refusedProposalStatus(err: ApiError): string | undefined {
  const status = (err.body as { proposal_status?: unknown } | undefined)?.proposal_status
  return typeof status === 'string' ? status : undefined
}

async function post(path: string): Promise<AssistantProposal> {
  const body = await apiRequest<{ proposal: ProposalApi }>(path, { method: 'POST' })
  return mapProposal(body.proposal)
}

export async function applyAssistantProposal(id: string): Promise<AssistantProposal> {
  const applied = await post(`/api/assistant/proposals/${encodeURIComponent(id)}/apply?today=${encodeURIComponent(todayISO())}`)
  // A planned stay Mate proposed is on the Forecast page the moment the
  // operator applies the card; any open page reads it again.
  if (applied.ops.some((op) => op.type === 'planned_stay')) void invalidateQueries([PLANNED_STAY_PATH])
  return applied
}

export function dismissAssistantProposal(id: string): Promise<AssistantProposal> {
  return post(`/api/assistant/proposals/${encodeURIComponent(id)}/dismiss`)
}
