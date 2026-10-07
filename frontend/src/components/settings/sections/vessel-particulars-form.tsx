import { FormRow, FormSection } from '@/components/patterns'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { useVesselIdentity } from '@/hooks/use-vessel-identity'
import { useVesselState } from '@/hooks/use-vessel-state'
import { useVesselParticularsFormContext } from '@/components/settings/vessel-particulars-context'
import type { VesselParticulars } from '@/hooks/use-vessel-particulars'

type TextField =
  | 'builder' | 'model' | 'hin' | 'flag' | 'hailing_port' | 'hull_type' | 'hull_material'
  | 'shore_power' | 'system_voltage' | 'registration' | 'imo' | 'epirb_id'

type OwnerField =
  | 'owner_name' | 'owner_phone' | 'owner_email' | 'insurer' | 'policy_number' | 'home_marina' | 'berth' | 'storm_delegate'

const OWNER_FIELDS: Array<{ id: OwnerField; label: string; type?: string }> = [
  { id: 'owner_name', label: 'Owner name' },
  { id: 'owner_phone', label: 'Owner phone', type: 'tel' },
  { id: 'owner_email', label: 'Owner email', type: 'email' },
  { id: 'insurer', label: 'Insurer' },
  { id: 'policy_number', label: 'Policy number' },
  { id: 'home_marina', label: 'Home marina' },
  { id: 'berth', label: 'Berth' },
  { id: 'storm_delegate', label: 'Storm delegate' },
]

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
 * Edits are held by VesselParticularsProvider and save with the page's Save
 * bar; the record is its own table, so the bar sends it as its own request.
 */
export function VesselParticularsForm() {
  const { draft, update, fieldError, loadError } = useVesselParticularsFormContext()
  const { boatName } = useVesselIdentity()
  const { vesselLengthOverallM, vesselDraftM } = useVesselState()

  if (loadError !== null) {
    return <p role="alert" className="text-sm text-destructive">{loadError}</p>
  }
  if (draft === null) {
    return <p className="text-sm text-muted-foreground">Loading particulars</p>
  }

  const errorFor = (field: keyof VesselParticulars) =>
    fieldError !== null && fieldError.field === field ? fieldError.message : null
  const fieldErrorText = (field: keyof VesselParticulars) => {
    const message = errorFor(field)
    return message === null
      ? null
      : <p id={`particulars-${field}-error`} className="text-xs text-destructive">{message}</p>
  }
  const invalidProps = (field: keyof VesselParticulars) =>
    errorFor(field) === null
      ? {}
      : { 'aria-invalid': true as const, 'aria-describedby': `particulars-${field}-error` }

  const live: Array<[string, string]> = [
    ['Name', boatName ?? '--'],
    ['Length overall', vesselLengthOverallM === null ? '--' : `${vesselLengthOverallM.toFixed(1)} m`],
    ['Draft', vesselDraftM === null ? '--' : `${vesselDraftM.toFixed(1)} m`],
  ]

  return (
    <>
      <FormSection
        title="Particulars"
        description="Name, call sign and MMSI come from the boat's live instrument data and are not edited here. Length overall is read from it when the boat publishes one; the figure below is kept for forms when it does not."
      >
        <dl className="grid grid-cols-1 gap-2 sm:grid-cols-3">
          {live.map(([label, value]) => (
            <div key={label} className="min-w-0 rounded-md border border-border/60 px-2.5 py-1.5">
              <dt className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</dt>
              <dd className="truncate text-sm tabular-nums">{value}</dd>
            </div>
          ))}
        </dl>
        <FormRow>
          {TEXT_FIELDS.map(({ id, label }) => (
            <Field key={id}>
              <FieldLabel htmlFor={`particulars-${id}`}>{label}</FieldLabel>
              <Input
                id={`particulars-${id}`}
                value={draft[id]}
                onChange={(e) => update({ [id]: e.target.value })}
                {...invalidProps(id)}
              />
              {fieldErrorText(id)}
            </Field>
          ))}
          <Field>
            <FieldLabel htmlFor="particulars-year">Year built</FieldLabel>
            <Input
              id="particulars-year"
              type="number"
              value={draft.year ?? ''}
              onChange={(e) => update({ year: numberOrNull(e.target.value) })}
              {...invalidProps('year')}
            />
            {fieldErrorText('year')}
          </Field>
          <Field>
            <FieldLabel htmlFor="particulars-displacement">Displacement (kg)</FieldLabel>
            <Input
              id="particulars-displacement"
              type="number"
              min={0}
              value={draft.displacement_kg ?? ''}
              onChange={(e) => update({ displacement_kg: numberOrNull(e.target.value) })}
              {...invalidProps('displacement_kg')}
            />
            {fieldErrorText('displacement_kg')}
          </Field>
          <Field>
            <FieldLabel htmlFor="particulars-loa">Length overall (m)</FieldLabel>
            <Input
              id="particulars-loa"
              type="number"
              min={0}
              step="0.1"
              value={draft.loa_m ?? ''}
              onChange={(e) => update({ loa_m: numberOrNull(e.target.value) })}
              {...invalidProps('loa_m')}
            />
            {fieldErrorText('loa_m')}
          </Field>
          <Field>
            <FieldLabel htmlFor="particulars-beam">Beam (m)</FieldLabel>
            <Input
              id="particulars-beam"
              type="number"
              min={0}
              step="0.1"
              value={draft.beam_m ?? ''}
              onChange={(e) => update({ beam_m: numberOrNull(e.target.value) })}
              {...invalidProps('beam_m')}
            />
            {fieldErrorText('beam_m')}
          </Field>
          <Field>
            <FieldLabel htmlFor="particulars-date-acquired">Date acquired</FieldLabel>
            <Input
              id="particulars-date-acquired"
              type="date"
              value={draft.date_acquired}
              onChange={(e) => update({ date_acquired: e.target.value })}
              {...invalidProps('date_acquired')}
            />
            {fieldErrorText('date_acquired')}
          </Field>
        </FormRow>
      </FormSection>
      <FormSection
        title="Owner & insurance"
        description="What insurers and marinas ask for again and again. Mate reads these when it fills in a form for you, and asks you for anything left blank."
      >
        <FormRow>
          {OWNER_FIELDS.map(({ id, label, type }) => (
            <Field key={id}>
              <FieldLabel htmlFor={`particulars-${id}`}>{label}</FieldLabel>
              <Input
                id={`particulars-${id}`}
                type={type}
                value={draft[id]}
                onChange={(e) => update({ [id]: e.target.value })}
                {...invalidProps(id)}
              />
              {fieldErrorText(id)}
            </Field>
          ))}
        </FormRow>
      </FormSection>
    </>
  )
}
