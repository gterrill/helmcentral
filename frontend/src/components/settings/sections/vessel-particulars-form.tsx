import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { useVesselIdentity } from '@/hooks/use-vessel-identity'
import { useVesselState } from '@/hooks/use-vessel-state'
import { useVesselParticulars, type VesselParticulars } from '@/hooks/use-vessel-particulars'

type TextField =
  | 'builder' | 'model' | 'hin' | 'flag' | 'hailing_port' | 'hull_type' | 'hull_material'
  | 'shore_power' | 'system_voltage' | 'registration' | 'imo' | 'epirb_id'

const TEXT_FIELDS: Array<{ id: TextField; label: string }> = [
  { id: 'builder', label: 'Builder' },
  { id: 'model', label: 'Model' },
  { id: 'hull_type', label: 'Hull type' },
  { id: 'hull_material', label: 'Hull material' },
  { id: 'hin', label: 'Hull ID (HIN)' },
  { id: 'flag', label: 'Flag' },
  { id: 'hailing_port', label: 'Hailing port' },
  { id: 'registration', label: 'Registration' },
  { id: 'imo', label: 'IMO number' },
  { id: 'epirb_id', label: 'EPIRB beacon ID' },
  { id: 'shore_power', label: 'Shore power' },
  { id: 'system_voltage', label: 'System voltage' },
]

function numberOrNull(raw: string): number | null {
  const trimmed = raw.trim()
  if (trimmed === '') return null
  const n = Number(trimmed)
  return Number.isFinite(n) ? n : null
}

/**
 * Settings -> Vessel: the particulars Helmcentral keeps for the boat, with the
 * values live instrument data already supplies shown read only beside them.
 * Saves on its own button: it is a separate record from the settings file, so
 * the page's Save Settings does not cover it.
 */
export function VesselParticularsForm() {
  const { particulars, error, save } = useVesselParticulars()
  const [draft, setDraft] = useState<VesselParticulars | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)
  const { boatName } = useVesselIdentity()
  const { vesselLengthOverallM, vesselDraftM } = useVesselState()

  useEffect(() => {
    if (particulars !== null) setDraft(particulars)
  }, [particulars])

  if (error !== null) {
    return <p role="alert" className="text-sm text-destructive">{error}</p>
  }
  if (draft === null) {
    return <p className="text-sm text-muted-foreground">Loading particulars</p>
  }

  const update = (patch: Partial<VesselParticulars>) => {
    setSaved(false)
    setDraft((previous) => (previous === null ? previous : { ...previous, ...patch }))
  }

  const handleSave = async () => {
    setSaving(true)
    setSaveError(null)
    setSaved(false)
    try {
      await save(draft)
      setSaved(true)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  const live: Array<[string, string]> = [
    ['Name', boatName ?? '--'],
    ['Length overall', vesselLengthOverallM === null ? '--' : `${vesselLengthOverallM.toFixed(1)} m`],
    ['Draft', vesselDraftM === null ? '--' : `${vesselDraftM.toFixed(1)} m`],
  ]

  return (
    <FieldSet>
      <FieldLegend variant="label">Particulars</FieldLegend>
      <p className="text-xs text-muted-foreground">
        Name, call sign, MMSI and the main dimensions come from the boat&apos;s live instrument data and are not edited here.
      </p>
      <dl className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-3">
        {live.map(([label, value]) => (
          <div key={label} className="min-w-0 rounded-md border border-border/60 px-2.5 py-1.5">
            <dt className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</dt>
            <dd className="truncate text-sm tabular-nums">{value}</dd>
          </div>
        ))}
      </dl>
      <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
        {TEXT_FIELDS.map(({ id, label }) => (
          <Field key={id}>
            <FieldLabel htmlFor={`particulars-${id}`}>{label}</FieldLabel>
            <Input
              id={`particulars-${id}`}
              value={draft[id]}
              onChange={(e) => update({ [id]: e.target.value })}
            />
          </Field>
        ))}
        <Field>
          <FieldLabel htmlFor="particulars-year">Year built</FieldLabel>
          <Input
            id="particulars-year"
            type="number"
            value={draft.year ?? ''}
            onChange={(e) => update({ year: numberOrNull(e.target.value) })}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="particulars-displacement">Displacement (kg)</FieldLabel>
          <Input
            id="particulars-displacement"
            type="number"
            min={0}
            value={draft.displacement_kg ?? ''}
            onChange={(e) => update({ displacement_kg: numberOrNull(e.target.value) })}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="particulars-date-acquired">Date acquired</FieldLabel>
          <Input
            id="particulars-date-acquired"
            type="date"
            value={draft.date_acquired}
            onChange={(e) => update({ date_acquired: e.target.value })}
          />
        </Field>
      </div>
      <div className="mt-3 flex items-center gap-3">
        <Button type="button" variant="outline" onClick={() => void handleSave()} disabled={saving}>
          {saving ? 'Saving' : 'Save particulars'}
        </Button>
        {saved && <span role="status" className="text-xs text-muted-foreground">Particulars saved</span>}
      </div>
      {saveError !== null && <p role="alert" className="mt-2 text-sm text-destructive">{saveError}</p>}
    </FieldSet>
  )
}
