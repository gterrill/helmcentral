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
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { ProcedureNoteField } from '@/components/inventory/maintenance-procedure-note-field'
import { intervalSummary, OVERRIDE_FIELD_LABELS } from '@/components/inventory/maintenance-provenance'
import {
  resetMaintenanceRuleOverride,
  setMaintenanceRuleOverrides,
  MAINTENANCE_DEFAULT_DUE_SOON_HOURS,
  MAINTENANCE_DEFAULT_DUE_SOON_MONTHS,
  type MaintenanceOverrideField,
  type MaintenanceOverridesInput,
  type MaintenanceRule,
} from '@/hooks/use-maintenance'

// ADR 0148: a profile job's description and intervals belong to the profile.
// This dialog shows the profile's own value under each one; typing a
// different value overrides it for this item alone, and an overridden value
// offers "Reset to profile". There is no Delete: the job is removed by
// editing the profile, or set aside here with "Not applicable to this item".

interface Draft {
  description: string
  interval_hours: string
  interval_months: string
  not_applicable: boolean
  due_soon_hours: string
  due_soon_months: string
  fixed_due_date: string
}

function draftOf(rule: MaintenanceRule): Draft {
  return {
    description: rule.description,
    interval_hours: rule.interval_hours == null ? '' : String(rule.interval_hours),
    interval_months: rule.interval_months == null ? '' : String(rule.interval_months),
    not_applicable: rule.not_applicable,
    due_soon_hours: rule.due_soon_hours == null ? '' : String(rule.due_soon_hours),
    due_soon_months: rule.due_soon_months == null ? '' : String(rule.due_soon_months),
    fixed_due_date: rule.fixed_due_date,
  }
}

const numberOrNull = (text: string): number | null => (text.trim() === '' ? null : Number(text))

/** Only what the operator changed - an unchanged value must not become an
 * override just because the dialog was saved. */
function changesOf(draft: Draft, rule: MaintenanceRule): MaintenanceOverridesInput {
  const before = draftOf(rule)
  const out: MaintenanceOverridesInput = {}
  if (draft.description !== before.description) out.description = draft.description
  if (draft.interval_hours !== before.interval_hours) out.interval_hours = numberOrNull(draft.interval_hours)
  if (draft.interval_months !== before.interval_months) out.interval_months = numberOrNull(draft.interval_months)
  if (draft.not_applicable !== before.not_applicable) out.not_applicable = draft.not_applicable
  if (draft.due_soon_hours !== before.due_soon_hours) out.due_soon_hours = numberOrNull(draft.due_soon_hours)
  if (draft.due_soon_months !== before.due_soon_months) out.due_soon_months = numberOrNull(draft.due_soon_months)
  if (draft.fixed_due_date !== before.fixed_due_date) out.fixed_due_date = draft.fixed_due_date === '' ? null : draft.fixed_due_date
  return out
}

interface MaintenanceProfileJobDialogProps {
  rule: MaintenanceRule
  open: boolean
  onCancel: () => void
  /** A write landed: the caller re-reads its list. */
  onChanged: (rule: MaintenanceRule) => void
}

export function MaintenanceProfileJobDialog({ rule, open, onCancel, onChanged }: MaintenanceProfileJobDialogProps) {
  const [liveRule, setLiveRule] = useState(rule)
  const [draft, setDraft] = useState<Draft>(() => draftOf(rule))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (open) {
      setLiveRule(rule)
      setDraft(draftOf(rule))
      setError(null)
    }
    // Re-seeded when the dialog opens on a different job, never on every
    // refresh of the list behind it (that would discard typing).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, rule.id])

  const overridden = (field: MaintenanceOverrideField) => liveRule.overridden_fields.includes(field)
  const profile = liveRule.profile_values
  const changes = changesOf(draft, liveRule)
  const hasChanges = Object.keys(changes).length > 0

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    try {
      const updated = await setMaintenanceRuleOverrides(liveRule.id, changes)
      onChanged(updated)
      onCancel()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  const handleReset = async (field: MaintenanceOverrideField) => {
    setError(null)
    try {
      const updated = await resetMaintenanceRuleOverride(liveRule.id, field)
      setLiveRule(updated)
      // Only the reset field follows the server; anything else typed stays.
      const fresh = draftOf(updated)
      setDraft((prev) => ({ ...prev, [field]: fresh[field] }))
      onChanged(updated)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  const resetButton = (field: MaintenanceOverrideField) =>
    overridden(field) ? (
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="h-auto self-start px-1 py-0 text-xs"
        aria-label={`Reset ${OVERRIDE_FIELD_LABELS[field]} to profile`}
        onClick={() => { void handleReset(field) }}
      >
        Reset to profile
      </Button>
    ) : null

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) onCancel() }}>
      <DialogContent className="max-h-[85vh] max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Edit profile job</DialogTitle>
          <DialogDescription>
            {liveRule.equipment_name}: this job comes from its equipment profile. Change a value here
            to differ for this item only; everything else follows the profile.
          </DialogDescription>
        </DialogHeader>

        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="profile-job-description">Description</FieldLabel>
            <Input
              id="profile-job-description"
              value={draft.description}
              onChange={(e) => setDraft((p) => ({ ...p, description: e.target.value }))}
            />
            {profile && <FieldDescription>Profile: {profile.description}</FieldDescription>}
            {resetButton('description')}
          </Field>

          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel htmlFor="profile-job-interval-hours">Every (hours)</FieldLabel>
              <Input
                id="profile-job-interval-hours"
                type="number"
                inputMode="decimal"
                min={0}
                value={draft.interval_hours}
                onChange={(e) => setDraft((p) => ({ ...p, interval_hours: e.target.value }))}
              />
              {profile && <FieldDescription>Profile: {profile.interval_hours == null ? 'not set' : `${profile.interval_hours} h`}</FieldDescription>}
              {resetButton('interval_hours')}
            </Field>
            <Field>
              <FieldLabel htmlFor="profile-job-interval-months">Every (months)</FieldLabel>
              <Input
                id="profile-job-interval-months"
                type="number"
                inputMode="numeric"
                min={0}
                value={draft.interval_months}
                onChange={(e) => setDraft((p) => ({ ...p, interval_months: e.target.value }))}
              />
              {profile && <FieldDescription>Profile: {profile.interval_months == null ? 'not set' : `${profile.interval_months} months`}</FieldDescription>}
              {resetButton('interval_months')}
            </Field>
          </div>
          {liveRule.first_at_hours != null && (
            <p className="text-xs text-muted-foreground">
              The profile's first service is at {liveRule.first_at_hours} h
              {intervalSummary(liveRule) ? `, then every ${intervalSummary(liveRule)}` : ''}.
            </p>
          )}

          <Field>
            <div className="flex items-center justify-between gap-3">
              <FieldLabel htmlFor="profile-job-not-applicable">Not applicable to this item</FieldLabel>
              <Switch
                id="profile-job-not-applicable"
                aria-label="Not applicable to this item"
                checked={draft.not_applicable}
                onCheckedChange={(checked) => setDraft((p) => ({ ...p, not_applicable: checked }))}
              />
            </div>
            <FieldDescription>Keeps the job in the list but never due, for work this item doesn't need.</FieldDescription>
            {resetButton('not_applicable')}
          </Field>

          <Field>
            <FieldLabel htmlFor="profile-job-fixed-date">Or a fixed due date</FieldLabel>
            <Input
              id="profile-job-fixed-date"
              type="date"
              value={draft.fixed_due_date}
              onChange={(e) => setDraft((p) => ({ ...p, fixed_due_date: e.target.value }))}
            />
          </Field>

          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel htmlFor="profile-job-due-soon-hours">Due soon within (hours)</FieldLabel>
              <Input
                id="profile-job-due-soon-hours"
                type="number"
                inputMode="decimal"
                min={0}
                placeholder={String(MAINTENANCE_DEFAULT_DUE_SOON_HOURS)}
                value={draft.due_soon_hours}
                onChange={(e) => setDraft((p) => ({ ...p, due_soon_hours: e.target.value }))}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="profile-job-due-soon-months">Due soon within (months)</FieldLabel>
              <Input
                id="profile-job-due-soon-months"
                type="number"
                inputMode="numeric"
                min={0}
                placeholder={String(MAINTENANCE_DEFAULT_DUE_SOON_MONTHS)}
                value={draft.due_soon_months}
                onChange={(e) => setDraft((p) => ({ ...p, due_soon_months: e.target.value }))}
              />
            </Field>
          </div>

          <ProcedureNoteField
            rule={liveRule}
            onChanged={(updated) => { setLiveRule(updated); onChanged(updated) }}
            onError={setError}
          />
        </FieldGroup>

        {error && <FieldError errors={[{ message: error }]} />}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={onCancel} disabled={saving}>Cancel</Button>
          <Button type="button" disabled={saving || !hasChanges || draft.description.trim() === ''} onClick={() => { void handleSave() }}>
            {saving ? 'Saving...' : 'Save'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
