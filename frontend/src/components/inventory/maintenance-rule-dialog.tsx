import { useEffect, useState } from 'react'
import { Trash2 } from 'lucide-react'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useEquipment } from '@/hooks/use-inventory'
import { MaintenanceProfileJobDialog } from '@/components/inventory/maintenance-profile-job-dialog'
import { ProcedureNoteField } from '@/components/inventory/maintenance-procedure-note-field'
import {
  isProfileJob,
  MAINTENANCE_DEFAULT_DUE_SOON_HOURS,
  MAINTENANCE_DEFAULT_DUE_SOON_MONTHS,
  type MaintenanceRule,
  type MaintenanceRuleInput,
} from '@/hooks/use-maintenance'

// ADR 0138: create or edit a rule's own core fields (spec §1). A rule
// belongs to one item, optionally none (a calendar-only certificate/expiry
// rule) - the item picker's blank option is exactly that "none" choice, not
// an unset/loading state. Procedure-note linking (spec §8) lives inside
// this dialog's edit view only - creating the note needs the rule's own id,
// which a brand new draft doesn't have until the first Save.

const NO_EQUIPMENT_VALUE = '__none__'

interface MaintenanceRuleDialogProps {
  /** null: closed. undefined: creating a fresh rule. A MaintenanceRule:
   * editing that one. */
  rule: MaintenanceRule | null | undefined
  /** Preselects the item picker for "New rule" launched from an item's own
   * Maintenance block - undefined/omitted for the plain list's own button. */
  presetEquipmentId?: string
  open: boolean
  onCancel: () => void
  onSave: (input: MaintenanceRuleInput) => Promise<MaintenanceRule>
  onDelete?: (id: string) => Promise<void>
  /** Fired after a procedure-note write (create or clear) succeeds, so the
   * caller's own rule list re-reads the fresh procedure_note_id - this
   * dialog only ever mutates local state otherwise. */
  onRuleChanged?: (rule: MaintenanceRule) => void
}

function draftFromRule(rule: MaintenanceRule | undefined, presetEquipmentId?: string): MaintenanceRuleInput {
  if (!rule) {
    return {
      equipment_id: presetEquipmentId ?? null,
      description: '',
      interval_hours: null,
      interval_months: null,
      due_soon_hours: null,
      due_soon_months: null,
      fixed_due_date: '',
      profile_service_id: '',
    }
  }
  return {
    equipment_id: rule.equipment_id,
    description: rule.description,
    interval_hours: rule.interval_hours,
    interval_months: rule.interval_months,
    due_soon_hours: rule.due_soon_hours,
    due_soon_months: rule.due_soon_months,
    fixed_due_date: rule.fixed_due_date,
    profile_service_id: rule.profile_service_id,
  }
}

function HandRuleDialog({ rule, presetEquipmentId, open, onCancel, onSave, onDelete, onRuleChanged }: MaintenanceRuleDialogProps) {
  const [draft, setDraft] = useState<MaintenanceRuleInput>(() => draftFromRule(rule ?? undefined, presetEquipmentId))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [pendingDelete, setPendingDelete] = useState(false)
  const [liveRule, setLiveRule] = useState(rule ?? null)

  const { items } = useEquipment(open ? {} : null)

  useEffect(() => {
    if (open) {
      setDraft(draftFromRule(rule ?? undefined, presetEquipmentId))
      setLiveRule(rule ?? null)
      setError(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, rule?.id])

  const isEditing = Boolean(rule)

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    try {
      await onSave(draft)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <Dialog open={open} onOpenChange={(next) => { if (!next) onCancel() }}>
        <DialogContent className="max-h-[85vh] max-w-lg overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{isEditing ? 'Edit rule' : 'New rule'}</DialogTitle>
            <DialogDescription>
              A rule needs an hours interval, a calendar interval, or both - whichever comes
              due first is what shows.
            </DialogDescription>
          </DialogHeader>

          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="maintenance-rule-item">Item</FieldLabel>
              <Select
                value={draft.equipment_id ?? NO_EQUIPMENT_VALUE}
                onValueChange={(value) => setDraft((p) => ({ ...p, equipment_id: value === NO_EQUIPMENT_VALUE ? null : value }))}
              >
                <SelectTrigger id="maintenance-rule-item" aria-label="Item">
                  <SelectValue>
                    {(value: string) => value === NO_EQUIPMENT_VALUE ? 'No item (certificate or expiry)' : (items.find((i) => i.id === value)?.name ?? value)}
                  </SelectValue>
                </SelectTrigger>
                <SelectPopup>
                  <SelectItem value={NO_EQUIPMENT_VALUE}>No item (certificate or expiry)</SelectItem>
                  {items.map((item) => (
                    <SelectItem key={item.id} value={item.id}>{item.name}</SelectItem>
                  ))}
                </SelectPopup>
              </Select>
              <FieldDescription>
                Leave blank for something that isn't a piece of gear - flares, EPIRB battery,
                registration.
              </FieldDescription>
            </Field>

            <Field>
              <FieldLabel htmlFor="maintenance-rule-description">Description</FieldLabel>
              <Input
                id="maintenance-rule-description"
                value={draft.description}
                onChange={(e) => setDraft((p) => ({ ...p, description: e.target.value }))}
                placeholder="Engine oil and filter"
              />
            </Field>

            <div className="grid grid-cols-2 gap-3">
              <Field>
                <FieldLabel htmlFor="maintenance-rule-interval-hours">Every (hours)</FieldLabel>
                <Input
                  id="maintenance-rule-interval-hours"
                  type="number"
                  inputMode="decimal"
                  min={0}
                  disabled={draft.equipment_id === null}
                  value={draft.interval_hours ?? ''}
                  onChange={(e) => setDraft((p) => ({ ...p, interval_hours: e.target.value === '' ? null : Number(e.target.value) }))}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="maintenance-rule-interval-months">Every (months)</FieldLabel>
                <Input
                  id="maintenance-rule-interval-months"
                  type="number"
                  inputMode="numeric"
                  min={0}
                  value={draft.interval_months ?? ''}
                  onChange={(e) => setDraft((p) => ({ ...p, interval_months: e.target.value === '' ? null : Number(e.target.value) }))}
                />
              </Field>
            </div>

            <Field>
              <FieldLabel htmlFor="maintenance-rule-fixed-date">Or a fixed due date</FieldLabel>
              <Input
                id="maintenance-rule-fixed-date"
                type="date"
                value={draft.fixed_due_date}
                onChange={(e) => setDraft((p) => ({ ...p, fixed_due_date: e.target.value }))}
              />
              <FieldDescription>For a one-off expiry rather than a recurring interval.</FieldDescription>
            </Field>

            <div className="grid grid-cols-2 gap-3">
              <Field>
                <FieldLabel htmlFor="maintenance-rule-due-soon-hours">Due soon within (hours)</FieldLabel>
                <Input
                  id="maintenance-rule-due-soon-hours"
                  type="number"
                  inputMode="decimal"
                  min={0}
                  placeholder={String(MAINTENANCE_DEFAULT_DUE_SOON_HOURS)}
                  value={draft.due_soon_hours ?? ''}
                  onChange={(e) => setDraft((p) => ({ ...p, due_soon_hours: e.target.value === '' ? null : Number(e.target.value) }))}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="maintenance-rule-due-soon-months">Due soon within (months)</FieldLabel>
                <Input
                  id="maintenance-rule-due-soon-months"
                  type="number"
                  inputMode="numeric"
                  min={0}
                  placeholder={String(MAINTENANCE_DEFAULT_DUE_SOON_MONTHS)}
                  value={draft.due_soon_months ?? ''}
                  onChange={(e) => setDraft((p) => ({ ...p, due_soon_months: e.target.value === '' ? null : Number(e.target.value) }))}
                />
              </Field>
            </div>

            {isEditing && liveRule && (
              <ProcedureNoteField
                rule={liveRule}
                onChanged={(updated) => { setLiveRule(updated); onRuleChanged?.(updated) }}
                onError={setError}
              />
            )}
          </FieldGroup>

          {error && <FieldError errors={[{ message: error }]} />}

          <DialogFooter>
            {isEditing && onDelete && (
              <Button
                type="button"
                variant="ghost"
                className="mr-auto gap-2 text-destructive"
                onClick={() => setPendingDelete(true)}
                disabled={saving}
              >
                <Trash2 className="h-4 w-4" aria-hidden="true" />
                Delete
              </Button>
            )}
            <Button type="button" variant="outline" onClick={onCancel} disabled={saving}>Cancel</Button>
            <Button type="button" disabled={saving || draft.description.trim() === ''} onClick={() => { void handleSave() }}>
              {saving ? 'Saving...' : 'Save'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={pendingDelete} onOpenChange={setPendingDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete this rule?</AlertDialogTitle>
            <AlertDialogDescription>
              The service log stays - completed entries keep their own history. This only
              removes the rule itself.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={() => { if (rule) void onDelete?.(rule.id) }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

/** One entry point for both kinds of rule. A profile job (ADR 0148) is not
 * edited like a hand rule: its definition belongs to the profile, so it gets
 * the overrides dialog; everything else (and a brand new rule) keeps today's
 * form. */
export function MaintenanceRuleDialog(props: MaintenanceRuleDialogProps) {
  if (props.rule && isProfileJob(props.rule)) {
    return (
      <MaintenanceProfileJobDialog
        rule={props.rule}
        open={props.open}
        onCancel={props.onCancel}
        onChanged={(rule) => props.onRuleChanged?.(rule)}
      />
    )
  }
  return <HandRuleDialog {...props} />
}
