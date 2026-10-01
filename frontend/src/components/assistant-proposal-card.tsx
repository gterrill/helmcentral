import { Check, Loader2, Wrench } from 'lucide-react'
import { useCallback, useState } from 'react'

import { Button } from '@/components/ui/button'
import type { AssistantProposal } from '@/hooks/use-assistant-conversations'
import { applyAssistantProposal, dismissAssistantProposal, ProposalActionError } from '@/lib/assistant-proposal-actions'
import { cn } from '@/lib/utils'

// Where an applied change can be seen: the Maintenance section of Inventory.
// It has no per-rule URL, so every line shares this one link.
const MAINTENANCE_HREF = '/inventory/maintenance'

interface AssistantProposalCardProps {
  proposal: AssistantProposal
  /** Below write tier the card still shows what Mate proposed, with no way to
   * apply or dismiss it. */
  canWrite: boolean
  /** Called with the server's answer to a successful Apply or Dismiss, so the
   * thread holds the new status. */
  onChange: (proposal: AssistantProposal) => void
}

/**
 * The card under a Mate reply that proposes changes to the maintenance
 * schedule (ADR 0146). Mate writes nothing: Apply is the operator's tap, and
 * the server runs every change together or none. The card always renders from
 * the stored status, so a reloaded thread shows an applied or dismissed card
 * as it was left.
 *
 * A stale proposal (the schedule changed after Mate proposed) is a 409 from
 * Apply and is stored server-side as stale with its reason: the card shows the
 * reason with no buttons, before and after a reload. Anything else that fails
 * leaves Apply available to retry.
 */
export function AssistantProposalCard({ proposal, canWrite, onChange }: AssistantProposalCardProps) {
  const [busy, setBusy] = useState<'apply' | 'dismiss' | null>(null)
  const [error, setError] = useState<string | null>(null)

  const run = useCallback(
    async (kind: 'apply' | 'dismiss') => {
      setBusy(kind)
      setError(null)
      try {
        const updated =
          kind === 'apply' ? await applyAssistantProposal(proposal.id) : await dismissAssistantProposal(proposal.id)
        onChange(updated)
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err)
        // A 409 from Apply names the stored status it refused for. Stale
        // carries its reason; a dismissed proposal (dismissed in another tab)
        // shows as dismissed. The thread holds the same, so a reload agrees.
        if (kind === 'apply' && err instanceof ProposalActionError && err.status === 409) {
          if (err.proposalStatus === 'dismissed') {
            onChange({ ...proposal, status: 'dismissed' })
          } else {
            onChange({ ...proposal, status: 'stale', staleReason: message })
          }
        } else {
          setError(message)
        }
      } finally {
        setBusy(null)
      }
    },
    [proposal, onChange],
  )

  const applied = proposal.status === 'applied'
  const dismissed = proposal.status === 'dismissed'
  const stale = proposal.status === 'stale'
  const pending = proposal.status === 'pending'
  const changeCount = proposal.ops.length

  return (
    <section
      aria-label="Proposed maintenance changes"
      data-testid="assistant-proposal-card"
      data-status={proposal.status}
      className="flex min-w-0 flex-col gap-2 rounded-md border border-border bg-card p-3"
    >
      <div className="flex min-w-0 items-center gap-2">
        {applied ? (
          <Check className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        ) : (
          <Wrench className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        )}
        <span className="min-w-0 truncate text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
          {changeCount === 1 ? 'Proposed change' : `Proposed changes (${changeCount})`}
        </span>
        <span className="ml-auto shrink-0 text-xs text-muted-foreground">
          {applied ? 'Applied' : dismissed ? 'Dismissed' : stale ? 'Out of date' : 'Not applied yet'}
        </span>
      </div>

      <ul className="flex min-w-0 flex-col gap-1">
        {proposal.ops.map((op, index) => (
          <li
            key={index}
            className={cn('min-w-0 break-words text-sm', dismissed || stale ? 'text-muted-foreground' : 'text-foreground', dismissed && 'line-through')}
          >
            {applied ? (
              <a href={MAINTENANCE_HREF} className="text-primary underline underline-offset-2">
                {op.summary}
              </a>
            ) : (
              op.summary
            )}
          </li>
        ))}
      </ul>

      {pending && !canWrite && (
        <p className="text-xs text-muted-foreground">Read-only session. Applying these changes needs write access.</p>
      )}

      {stale && (
        <p role="alert" className="text-xs text-destructive">
          {proposal.staleReason} Ask Mate to redo it.
        </p>
      )}

      {error !== null && (
        <p role="alert" className="text-xs text-destructive">
          {error}
        </p>
      )}

      {pending && canWrite && (
        <div className="flex flex-wrap gap-2">
          <Button type="button" size="sm" disabled={busy !== null} onClick={() => { void run('apply') }}>
            {busy === 'apply' && <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />}
            {busy === 'apply' ? 'Applying…' : 'Apply'}
          </Button>
          <Button type="button" size="sm" variant="ghost" disabled={busy !== null} onClick={() => { void run('dismiss') }}>
            Dismiss
          </Button>
        </div>
      )}
    </section>
  )
}
