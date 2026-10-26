import { StateChip } from '@/components/assistant-passage-window-card'
import { PassageDecisionControl } from '@/components/passage-decision-control'
import { Button } from '@/components/ui/button'
import { useNavigate } from '@/hooks/use-app-location'
import { getQueryData, setQueryData } from '@/lib/api-query'
import { formatRowDate } from '@/lib/passage-dates'
import type { PassagePlan } from '@/lib/passage-plans'
import { PASSAGES_KEY } from '@/lib/passages-api'
import type { PassageDecision } from '@/lib/mate-cards'

export interface PassagePlansProps {
  passageId: string
  /** Null when the plans could not be read, with `error` saying why. */
  plans: PassagePlan[] | null
  error: string | null
  canWrite: boolean
}

// The cached passage is the server's own JSON, so the new view goes back in
// that shape. It shows at once. The save has already asked for the passage to
// be read again, and that read has the final say. A passage that is not cached
// has nothing to patch: the save succeeded, so it is simply read again.
function showDecision(passageId: string, view: PassageDecision) {
  const key = [...PASSAGES_KEY, passageId]
  if (getQueryData(key) === undefined) return
  setQueryData<{ passage: { planned_with_mate?: { decision: { id: number } }[] | null } }>(key, (prev) => {
    const plans = prev!.passage.planned_with_mate
    return {
      ...prev!,
      passage: {
        ...prev!.passage,
        planned_with_mate: plans?.map((p) => (p.decision.id === view.id
          ? { ...p, decision: { id: view.id, state: view.state, row_date: view.rowDate, inferred: view.inferred, passage_id: view.passageId } }
          : p)),
      },
    }
  })
}

function PlanLine({ passageId, plan, canWrite }: { passageId: string; plan: PassagePlan; canWrite: boolean }) {
  const navigate = useNavigate()
  const { decision, rowVerdict } = plan
  return (
    <li className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted-foreground">
      <span className="min-w-0">
        Planned with Mate{decision.rowDate ? ` · ${formatRowDate(decision.rowDate)}` : ''}{rowVerdict ? ' · ' : ''}
      </span>
      {rowVerdict && <StateChip state={rowVerdict} />}
      <PassageDecisionControl
        decision={decision}
        rowDates={plan.rowDates}
        canWrite={canWrite}
        onChange={(view) => showDecision(passageId, view)}
      />
      {plan.chatExists && (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-auto min-h-0 px-1 py-0.5 text-xs underline"
          onClick={() => { navigate({ panel: 'assistant', conversationId: plan.conversationId }) }}
        >
          Open chat
        </Button>
      )}
    </li>
  )
}

/** The Mate plans a passage was sailed from, one line each, or why they could not be read. */
export function PassagePlans({ passageId, plans, error, canWrite }: PassagePlansProps) {
  if (plans === null) {
    return <p role="alert" className="text-sm text-destructive">{error}</p>
  }
  if (plans.length === 0) return null
  return (
    <ul className="flex flex-col gap-1">
      {plans.map((p) => <PlanLine key={p.decision.id} passageId={passageId} plan={p} canWrite={canWrite} />)}
    </ul>
  )
}
