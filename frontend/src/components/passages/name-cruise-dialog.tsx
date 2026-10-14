import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export interface NameCruiseDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: string
  confirmLabel: string
  busy?: boolean
  onConfirm: (name: string) => void
}

export function NameCruiseDialog({ open, onOpenChange, title, description, confirmLabel, busy = false, onConfirm }: NameCruiseDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!busy) onOpenChange(next) }}>
      <DialogContent>
        {/* The form is its own component so a reopened dialog starts blank. */}
        {open && <NameForm {...{ title, description, confirmLabel, busy, onConfirm, onCancel: () => onOpenChange(false) }} />}
      </DialogContent>
    </Dialog>
  )
}

function NameForm({ title, description, confirmLabel, busy, onConfirm, onCancel }: Omit<NameCruiseDialogProps, 'open' | 'onOpenChange'> & { onCancel: () => void }) {
  const [name, setName] = useState('')
  const trimmed = name.trim()
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => { e.preventDefault(); if (trimmed) onConfirm(trimmed) }}
    >
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description ?? 'A Cruise groups passages, such as a season or a trip with guests.'}</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-2">
        <Label htmlFor="cruise-name">Cruise name</Label>
        <Input id="cruise-name" autoFocus value={name} onChange={(e) => setName(e.target.value)} maxLength={120} />
      </div>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onCancel}>Cancel</Button>
        <Button type="submit" disabled={busy || !trimmed}>{confirmLabel}</Button>
      </DialogFooter>
    </form>
  )
}
