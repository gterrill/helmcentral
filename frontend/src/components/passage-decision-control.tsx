import { useState } from 'react'
import { toast } from 'sonner'

import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { PassageDecision } from '@/lib/mate-cards'
import { formatRowDate } from '@/lib/passage-dates'
import { passageDecisionSaved, passageDecisionsApi, PassageDecisionUnreadableError, type PassageDecisionBody } from '@/lib/passage-decisions-api'

const WENT_PREFIX = 'went:'

function optionValue(d: PassageDecision): string {
  if (d.state === 'went' && d.rowDate) return `${WENT_PREFIX}${d.rowDate}`
  return d.state === '' ? 'not_set' : d.state
}

function textFor(d: PassageDecision): string {
  switch (d.state) {
    case 'went':
      return d.rowDate ? `Went · ${formatRowDate(d.rowDate)}` : 'Went'
    case 'stayed':
      return 'Stayed'
    case 'just_looking':
      return 'Just looking'
    default:
      return 'Not set'
  }
}

/** The words shown for a decision: "Went · Thu 23 Oct (inferred)". */
export function decisionText(d: PassageDecision): string {
  return d.inferred ? `${textFor(d)} (inferred)` : textFor(d)
}

function bodyFor(value: string): PassageDecisionBody {
  if (value.startsWith(WENT_PREFIX)) return { state: 'went', row_date: value.slice(WENT_PREFIX.length) }
  if (value === 'not_set') return { state: '' }
  return { state: value as 'stayed' | 'just_looking' }
}

interface PassageDecisionControlProps {
  decision: PassageDecision
  /** The departure dates (YYYY-MM-DD) the plan offered, one "Went" option each. */
  rowDates: readonly string[]
  canWrite: boolean
  /** Called with what the server answered, never before. */
  onChange: (decision: PassageDecision) => void
  /** Called when the server saved the choice but its answer could not be read, so the screen can be re-read. */
  onUnreadable?: () => void
}

/**
 * Says what was done with a Mate passage plan and, with write access, lets the
 * operator change it. Nothing changes on screen until the server has answered,
 * and a refusal shows the server's own words in a toast.
 */
export function PassageDecisionControl({ decision, rowDates, canWrite, onChange, onUnreadable }: PassageDecisionControlProps) {
  const [saving, setSaving] = useState(false)
  const text = decisionText(decision)
  if (!canWrite) return <span className="min-w-0 truncate text-xs text-foreground">{text}</span>

  const choose = async (value: string | null) => {
    if (value === null || value === optionValue(decision)) return
    setSaving(true)
    try {
      const view = await passageDecisionsApi.put(decision.id, bodyFor(value))
      onChange(view)
      void passageDecisionSaved(decision.passageId, view)
    } catch (err) {
      if (err instanceof PassageDecisionUnreadableError) {
        toast.error('Saved, but the answer could not be read', { description: 'Showing the saved plan again.' })
        void passageDecisionSaved(decision.passageId, null)
        onUnreadable?.()
        return
      }
      toast.error('Could not save that', { description: err instanceof Error ? err.message : String(err) })
    } finally {
      setSaving(false)
    }
  }

  return (
    <Select value={optionValue(decision)} onValueChange={choose} disabled={saving}>
      <SelectTrigger aria-label="What you did with this plan" className="h-8 w-auto min-w-0 max-w-full gap-2 px-2 py-1 text-xs">
        <SelectValue>{text}</SelectValue>
      </SelectTrigger>
      <SelectPopup>
        {rowDates.map((d) => (
          <SelectItem key={d} value={`${WENT_PREFIX}${d}`} className="text-xs">
            Went · {formatRowDate(d)}
          </SelectItem>
        ))}
        <SelectItem value="stayed" className="text-xs">Stayed</SelectItem>
        <SelectItem value="just_looking" className="text-xs">Just looking</SelectItem>
        <SelectItem value="not_set" className="text-xs">Not set</SelectItem>
      </SelectPopup>
    </Select>
  )
}
