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

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ProposalActionError'
    this.status = status
  }
}

async function post(path: string): Promise<AssistantProposal> {
  const response = await fetch(`${apiBaseUrl}${path}`, { method: 'POST' })
  if (!response.ok) {
    let message = `HTTP ${response.status}`
    try {
      const body = (await response.json()) as { error?: string; message?: string }
      message = body.message ?? body.error ?? message
    } catch {
      // Not JSON: keep the status line.
    }
    throw new ProposalActionError(response.status, message)
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
