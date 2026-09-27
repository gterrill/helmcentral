import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import type { MaintenanceRule } from '@/hooks/use-maintenance'

// ADR 0138: two small, single-purpose dialogs shared by the Maintenance
// list (maintenance-section.tsx) and the equipment editor's own
// Maintenance block - acknowledging a rule (a short reason, spec §5) and
// setting its baseline by hand for onboarding (spec §3), never faking a
// log entry. Kept in one file since both are a handful of fields around
// one Save/Cancel footer - the same "small enough to share a file" call
// this feature makes for maintenance-log-dialogs.tsx.

interface MaintenanceAckDialogProps {
  rule: MaintenanceRule | null
  onCancel: () => void
  onConfirm: (reason: string) => Promise<void>
}

/**
 * Acknowledge (a non-blank reason) or clear (blank/Clear) a rule's
 * acknowledgement. Opens pre-filled with the rule's own current ack_reason,
 * so re-opening an already-acknowledged rule shows what was said rather
 * than an empty box.
 */
export function MaintenanceAckDialog({ rule, onCancel, onConfirm }: MaintenanceAckDialogProps) {
  const [reason, setReason] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setReason(rule?.ack_reason ?? '')
    setError(null)
  }, [rule])

  const handleSave = async (nextReason: string) => {
    setSaving(true)
    setError(null)
    try {
      await onConfirm(nextReason)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={rule !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Acknowledge</DialogTitle>
          <DialogDescription>
            {rule?.description} stays overdue or due soon and still counts - acknowledging just
            says you know about it, with a short reason ("yard", "waiting on parts").
          </DialogDescription>
        </DialogHeader>

        <Field>
          <FieldLabel htmlFor="maintenance-ack-reason">Reason</FieldLabel>
          <Input
            id="maintenance-ack-reason"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="yard, waiting on parts..."
            autoFocus
          />
        </Field>
        {error && <FieldError errors={[{ message: error }]} />}

        <DialogFooter>
          {rule?.acknowledged && (
            <Button type="button" variant="outline" disabled={saving} onClick={() => { void handleSave('') }}>
              Clear
            </Button>
          )}
          <Button type="button" variant="outline" onClick={onCancel} disabled={saving}>Cancel</Button>
          <Button type="button" disabled={saving || reason.trim() === ''} onClick={() => { void handleSave(reason) }}>
            {saving ? 'Saving...' : 'Acknowledge'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

interface MaintenanceLastDoneDialogProps {
  rule: MaintenanceRule | null
  onCancel: () => void
  onConfirm: (lastDoneAt: string | undefined, lastDoneHours: number | undefined) => Promise<void>
}

/**
 * Set last done - spec §3's onboarding action for a rule that shows "never
 * recorded": a date and/or an hours reading, recorded as the baseline with
 * NO log entry written (this never claims a service happened through this
 * app - it only records what was already true before the rule existed
 * here).
 */
export function MaintenanceLastDoneDialog({ rule, onCancel, onConfirm }: MaintenanceLastDoneDialogProps) {
  const [date, setDate] = useState('')
  const [hours, setHours] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setDate('')
    setHours('')
    setError(null)
  }, [rule])

  const canSave = date.trim() !== '' || hours.trim() !== ''

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    try {
      await onConfirm(date.trim() || undefined, hours.trim() === '' ? undefined : Number(hours))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={rule !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Set last done</DialogTitle>
          <DialogDescription>
            Record what was already true for {rule?.description} before it was entered here -
            a date and/or an hours reading. This does not add a service log entry.
          </DialogDescription>
        </DialogHeader>

        <Field>
          <FieldLabel htmlFor="maintenance-last-done-date">Last done (date)</FieldLabel>
          <Input id="maintenance-last-done-date" type="date" value={date} onChange={(e) => setDate(e.target.value)} />
        </Field>
        {rule?.has_hour_meter_path && (
          <Field>
            <FieldLabel htmlFor="maintenance-last-done-hours">Last done (hours)</FieldLabel>
            <Input
              id="maintenance-last-done-hours"
              type="number"
              inputMode="decimal"
              min={0}
              value={hours}
              onChange={(e) => setHours(e.target.value)}
            />
          </Field>
        )}
        {error && <FieldError errors={[{ message: error }]} />}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={onCancel} disabled={saving}>Cancel</Button>
          <Button type="button" disabled={saving || !canSave} onClick={() => { void handleSave() }}>
            {saving ? 'Saving...' : 'Save'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
