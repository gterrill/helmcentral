import { ApiError, apiRequest, invalidateQueries } from '@/lib/api-query'
import { mapProposal, type AssistantProposal, type ProposalApi } from '@/hooks/use-assistant-conversations'
import { todayISO } from '@/lib/local-date'
import { PLANNED_STAY_PATH } from '@/hooks/use-planned-stay'
import { ALARM_RULES_PATH } from '@/hooks/use-alarm-rules'
import { invalidateWatches } from '@/hooks/use-watches'

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

/** When Apply saved the changes but said something went wrong afterwards (the
 * alarm rules did not reload), the server answers an error that still carries
 * the proposal, as applied. The change is real, so the card shows it applied
 * beside the reason. */
export function appliedProposalOf(err: unknown): AssistantProposal | undefined {
  if (!(err instanceof ApiError)) return undefined
  const proposal = (err.body as { proposal?: ProposalApi } | undefined)?.proposal
  return proposal && proposal.status === 'applied' ? mapProposal(proposal) : undefined
}

export async function applyAssistantProposal(id: string): Promise<AssistantProposal> {
  let applied: AssistantProposal
  try {
    applied = await post(`/api/assistant/proposals/${encodeURIComponent(id)}/apply?today=${encodeURIComponent(todayISO())}`)
  } catch (err) {
    const saved = appliedProposalOf(err)
    if (saved?.ops.some((op) => op.type === 'alarm_rule')) void invalidateQueries([ALARM_RULES_PATH])
    if (saved?.ops.some((op) => op.type === 'watch')) void invalidateWatches()
    throw err
  }
  // A planned stay Mate proposed is on the Forecast page the moment the
  // operator applies the card; any open page reads it again.
  if (applied.ops.some((op) => op.type === 'planned_stay')) void invalidateQueries([PLANNED_STAY_PATH])
  // An alarm rule Mate proposed is live the moment it is applied, so the
  // Alarms page and the battery tile's bands read the rules again.
  if (applied.ops.some((op) => op.type === 'alarm_rule')) void invalidateQueries([ALARM_RULES_PATH])
  // A watch Mate proposed is on the Watches page the moment it is applied.
  if (applied.ops.some((op) => op.type === 'watch')) void invalidateWatches()
  return applied
}

export function dismissAssistantProposal(id: string): Promise<AssistantProposal> {
  return post(`/api/assistant/proposals/${encodeURIComponent(id)}/dismiss`)
}
