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
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import type { HandHourReadingInput } from '@/hooks/use-maintenance'
import { errorMessage } from '@/lib/errors'
import { todayISO } from '@/lib/local-date'

// ADR 0268: Log a reading. For gear with no hour meter, the operator reads the
// gauge and types the figure here. Hours-based rules on the item count from
// the latest one. The figure is always what the gauge shows; the server turns
// it into true hours across any meter replacement.

interface HandHoursDialogProps {
  equipmentName: string
  open: boolean
  onCancel: () => void
  /** Rejects with the server's message, which the dialog shows and stays open on. */
  onSave: (input: HandHourReadingInput) => Promise<void>
}

export function HandHoursDialog({ equipmentName, open, onCancel, onSave }: HandHoursDialogProps) {
  const [readOn, setReadOn] = useState(todayISO())
  const [gauge, setGauge] = useState('')
  const [note, setNote] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setReadOn(todayISO())
    setGauge('')
    setNote('')
    setError(null)
  }, [open])

  const canSave = gauge.trim() !== '' && readOn !== '' && Number(gauge) >= 0

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    try {
      await onSave({ read_on: readOn, gauge: Number(gauge), note: note.trim() })
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(isOpen) => { if (!isOpen) onCancel() }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Log a reading</DialogTitle>
          <DialogDescription>
            {equipmentName} has no hour meter. Type what its gauge shows, and its hours-based rules count from the latest reading.
          </DialogDescription>
        </DialogHeader>

        <Field>
          <FieldLabel htmlFor="hand-hours-gauge">Gauge reading (hours)</FieldLabel>
          <Input
            id="hand-hours-gauge"
            type="number"
            inputMode="decimal"
            min={0}
            value={gauge}
            onChange={(e) => setGauge(e.target.value)}
            autoFocus
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="hand-hours-date">Date read</FieldLabel>
          <Input id="hand-hours-date" type="date" max={todayISO()} value={readOn} onChange={(e) => setReadOn(e.target.value)} />
        </Field>
        <Field>
          <FieldLabel htmlFor="hand-hours-note">Note (optional)</FieldLabel>
          <Input id="hand-hours-note" value={note} maxLength={500} onChange={(e) => setNote(e.target.value)} />
          <FieldDescription>It is kept with the reading.</FieldDescription>
        </Field>
        {error && <FieldError errors={[{ message: error }]} />}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={onCancel} disabled={saving}>Cancel</Button>
          <Button type="button" disabled={saving || !canSave} onClick={() => { void handleSave() }}>
            {saving ? 'Saving...' : 'Save reading'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
