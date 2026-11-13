import { ApiError, apiRequest, invalidateQueries } from '@/lib/api-query'
import { mapProposal, type AssistantProposal, type AssistantProposalOp, type ProposalApi } from '@/hooks/use-assistant-conversations'
import { todayISO } from '@/lib/local-date'
import { ALARM_RULES_PATH } from '@/hooks/use-alarm-rules'
import { SAVED_PLACES_PATH } from '@/hooks/use-saved-places'
import { WATCHES_PATH } from '@/hooks/use-watches'
import { MATE_MEMORIES_PATH } from '@/hooks/use-mate-memories'
import { decksRecordApplied, equipmentRecordApplied, zonesRecordApplied } from '@/hooks/use-inventory'
import { maintenanceLogRecordApplied, maintenanceRulesRecordApplied } from '@/hooks/use-maintenance'
import { openJobRecordApplied } from '@/hooks/use-open-jobs'
import { documentRecordApplied } from '@/hooks/use-documents'
import { vesselParticularsRecordApplied } from '@/hooks/use-vessel-particulars'

const reads = (path: string) => () => void invalidateQueries([path])

/**
 * What to read again after Apply, by op type. Apply writes the records, and any
 * open page that shows them asks again. Inventory and maintenance types call
 * the same refresh their own pages run after a direct write, so a delete skips
 * the keys that would answer 404 and an update reaches the details page. Every
 * record type the backend registers needs a line here (a backend test checks),
 * so a new type cannot leave its page stale after Apply.
 */
export const PROPOSAL_REFRESH: Record<string, (op: AssistantProposalOp) => void> = {
  alarm_rule: reads(ALARM_RULES_PATH),
  watch: reads(WATCHES_PATH),
  saved_place: reads(SAVED_PLACES_PATH),
  memory: reads(MATE_MEMORIES_PATH),
  equipment: (op) => equipmentRecordApplied(op.action, op.id),
  location: zonesRecordApplied,
  bin: zonesRecordApplied,
  deck: decksRecordApplied,
  maintenance_rule: maintenanceRulesRecordApplied,
  maintenance_log: maintenanceLogRecordApplied,
  open_job: openJobRecordApplied,
  vessel_particulars: vesselParticularsRecordApplied,
  document: (op) => documentRecordApplied(op.id),
}

function refreshPagesFor(proposal: AssistantProposal | undefined): void {
  for (const op of proposal?.ops ?? []) {
    const refresh = PROPOSAL_REFRESH[op.type]
    if (refresh) refresh(op)
    else console.error(`Apply changed a "${op.type}" record but no pages are listed to refresh for it`)
  }
}

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
    refreshPagesFor(appliedProposalOf(err))
    throw err
  }
  refreshPagesFor(applied)
  return applied
}

export function dismissAssistantProposal(id: string): Promise<AssistantProposal> {
  return post(`/api/assistant/proposals/${encodeURIComponent(id)}/dismiss`)
}

/** Undo on a card for a fact Mate kept: the server removes the fact and stores
 * the proposal as undone, or refuses (a 409 with its reason) when the fact was
 * rewritten since. */
export async function undoAssistantProposal(id: string): Promise<AssistantProposal> {
  const undone = await post(`/api/assistant/proposals/${encodeURIComponent(id)}/undo`)
  refreshPagesFor(undone)
  return undone
}