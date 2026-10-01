import { apiBaseUrl } from '@/config/api'
import { mapProposal, type AssistantProposal, type ProposalApi } from '@/hooks/use-assistant-conversations'
import { todayISO } from '@/lib/local-date'

/**
 * Apply and Dismiss on a maintenance proposal card (ADR 0146). Apply is the
 * one place Mate's proposed changes are written, so it carries the same
 * `?today=` every maintenance write does (the operator's own local date, never
 * the server's).
 */

/** A refused apply or dismiss. `status` 409 means the proposal can no longer
 * be applied as written (a rule changed since Mate proposed it, or it was
 * already dismissed or applied); `message` is the server's own reason,
 * verbatim. */
export class ProposalActionError extends Error {
  readonly status: number
  /** On a 409, which stored status the server refused for: 'stale',
   * 'dismissed' or 'applied'. */
  readonly proposalStatus: string | undefined

  constructor(status: number, message: string, proposalStatus?: string) {
    super(message)
    this.name = 'ProposalActionError'
    this.status = status
    this.proposalStatus = proposalStatus
  }
}

async function post(path: string): Promise<AssistantProposal> {
  const response = await fetch(`${apiBaseUrl}${path}`, { method: 'POST' })
  if (!response.ok) {
    let message = `HTTP ${response.status}`
    let proposalStatus: string | undefined
    try {
      const body = (await response.json()) as { error?: string; message?: string; proposal_status?: string }
      message = body.message ?? body.error ?? message
      proposalStatus = body.proposal_status
    } catch {
      // Not JSON: keep the status line.
    }
    throw new ProposalActionError(response.status, message, proposalStatus)
  }
  const body = (await response.json()) as { proposal: ProposalApi }
  return mapProposal(body.proposal)
}

export function applyAssistantProposal(id: string): Promise<AssistantProposal> {
  return post(`/api/assistant/proposals/${encodeURIComponent(id)}/apply?today=${encodeURIComponent(todayISO())}`)
}

export function dismissAssistantProposal(id: string): Promise<AssistantProposal> {
  return post(`/api/assistant/proposals/${encodeURIComponent(id)}/dismiss`)
}
