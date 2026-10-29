import { ApiError, apiRequest, invalidateQueries } from '@/lib/api-query'
import { mapProposal, type AssistantProposal, type ProposalApi } from '@/hooks/use-assistant-conversations'
import { todayISO } from '@/lib/local-date'
import { PLANNED_STAY_PATH } from '@/hooks/use-planned-stay'
import { ALARM_RULES_PATH } from '@/hooks/use-alarm-rules'
import { WATCHES_PATH } from '@/hooks/use-watches'
import { DECKS, DISCOVERY, EQUIPMENT, MAINTENANCE_RULES, SETUP_STATUS, ZONES } from '@/hooks/use-inventory'
import { LOG as MAINTENANCE_LOG } from '@/hooks/use-maintenance'
import { PATH as VESSEL_PARTICULARS_PATH } from '@/hooks/use-vessel-particulars'

const SETTINGS_PATH = '/api/settings'

/**
 * The cached reads each kind of change touches, by op type. Apply writes the
 * records, and any open page that reads them asks again. Every record type the
 * backend registers needs a line here (a backend test checks), so a new type
 * cannot leave its page stale after Apply.
 *
 * A bin lives inside its zone, so bins and zones both reload ZONES. A deck
 * holds zones' outlines, and equipment sits in zones and bins, so each reloads
 * the other's lists.
 */
export const PROPOSAL_REFRESH: Record<string, readonly string[]> = {
  planned_stay: [PLANNED_STAY_PATH],
  alarm_rule: [ALARM_RULES_PATH],
  watch: [WATCHES_PATH],
  equipment: [EQUIPMENT, ZONES, MAINTENANCE_RULES, DISCOVERY, SETUP_STATUS, SETTINGS_PATH],
  location: [ZONES, DECKS, EQUIPMENT],
  bin: [ZONES, EQUIPMENT],
  deck: [DECKS, ZONES],
  maintenance_rule: [MAINTENANCE_RULES],
  maintenance_log: [MAINTENANCE_RULES, MAINTENANCE_LOG],
  vessel_particulars: [VESSEL_PARTICULARS_PATH],
}

function refreshPagesFor(proposal: AssistantProposal | undefined): void {
  const paths = new Set<string>()
  for (const op of proposal?.ops ?? []) {
    const entry = PROPOSAL_REFRESH[op.type]
    if (!entry) {
      console.error(`Apply changed a "${op.type}" record but no pages are listed to refresh for it`)
      continue
    }
    for (const path of entry) paths.add(path)
  }
  for (const path of paths) void invalidateQueries([path])
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
