import { useMemo } from 'react'

import { FormSection } from '@/components/patterns'
import { FieldLabel } from '@/components/ui/field'
import { useSettingsFormContext } from '@/components/settings/settings-form-context'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'
import { useEquipment, type EquipmentItem } from '@/hooks/use-inventory'
import { useVesselDiscovery } from '@/hooks/use-vessel-discovery'
import { discoveryRows, type SourceKind, type VesselDetectorStatus, type VesselRoles } from '@/lib/vessel-settings'

interface SignalKRolesSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

/** The item-valued roles, in the order they are shown, with the kind of item each takes. */
const ITEM_ROLES: { field: Exclude<keyof VesselRoles, 'generator_ac_instance'>; label: string; kind: SourceKind }[] = [
  { field: 'house_bank', label: 'House bank', kind: 'battery' },
  { field: 'voltage_readout_battery', label: 'Voltage readout battery', kind: 'battery' },
  { field: 'port_alternator', label: 'Port alternator', kind: 'alternator' },
  { field: 'starboard_alternator', label: 'Starboard alternator', kind: 'alternator' },
  { field: 'shore_charger', label: 'Shore charger', kind: 'charger' },
  { field: 'inverter', label: 'Inverter', kind: 'inverter' },
  { field: 'generator', label: 'Generator', kind: 'generator' },
]

function detectorLine(label: string, status: VesselDetectorStatus | undefined) {
  const ready = status?.ready === true
  return (
    <div key={label} className="flex min-w-0 items-center gap-2 rounded-md border border-border/60 bg-background/40 px-2.5 py-1.5">
      <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${ready ? 'bg-emerald-500' : 'bg-muted-foreground/40'}`} />
      <span className="min-w-0 truncate text-xs text-muted-foreground">
        <span className="font-medium text-foreground">{label}</span>
        {': '}
        {ready ? 'Ready' : `Not set up${status?.missing ? `. ${status.missing}` : ''}`}
      </span>
    </div>
  )
}

/**
 * Settings -> SignalK -> Roles: the single-choice parts the electrical tiles,
 * the overnight model and the anomaly checks need. Each is a dropdown over the
 * equipment items assigned to that kind of source. The choices belong to the
 * settings draft and save with the page's Save bar.
 */
export function SignalKRolesSection({ draft, onChange }: SignalKRolesSectionProps) {
  const { settings: savedSettings, loading: settingsLoading } = useSettingsFormContext()
  const { items } = useEquipment({})
  const { discovery } = useVesselDiscovery()
  const roles = draft.roles

  const itemsByKind = useMemo(() => {
    const byKind = new Map<string, EquipmentItem[]>()
    for (const item of items) {
      if (item.source_kind === '') continue
      byKind.set(item.source_kind, [...(byKind.get(item.source_kind) ?? []), item])
    }
    for (const list of byKind.values()) list.sort((a, b) => a.name.localeCompare(b.name))
    return byKind
  }, [items])

  if (settingsLoading) {
    return (
      <FormSection title="Roles">
        <p className="text-sm text-muted-foreground">Loading…</p>
      </FormSection>
    )
  }

  // The server named a problem with the stored block. The draft stays null, so
  // a save leaves the block alone, and the operator is told why. Only this
  // explicit error raises the alert: a null draft with no error is just the
  // page hydrating a render after settings arrive.
  if (roles === null) {
    return (
      <FormSection title="Roles">
        {savedSettings.roles_error ? (
          <p role="alert" className="text-sm text-destructive">
            Roles could not be read: {savedSettings.roles_error}. They are not shown and will not be changed by a save.
          </p>
        ) : (
          <p className="text-sm text-muted-foreground">Loading…</p>
        )}
      </FormSection>
    )
  }

  const setRole = (patch: Partial<VesselRoles>) => onChange({ roles: { ...roles, ...patch } })
  const acRows = discoveryRows(discovery, 'ac')
  const acMissing = roles.generator_ac_instance !== '' && !acRows.some((r) => r.instance === roles.generator_ac_instance)

  return (
    <FormSection
      title="Roles"
      description="Pick which equipment item plays each part. The Battery & Power tile, its dawn projection and state-of-charge bands, the Alternators tile, the Generator tile and the anomaly checks read these. Anything left as Not set shows no data. Each list holds the items assigned to that kind of source in Discovery above."
    >
      <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2">
        {ITEM_ROLES.map(({ field, label, kind }) => {
          const options = itemsByKind.get(kind) ?? []
          const value = roles[field]
          // A saved choice whose item has lost its source, or been deleted,
          // stays visible so the operator sees what is stored instead of a
          // silently blank choice.
          const missing = value !== '' && !options.some((item) => item.id === value)
          const otherAlternator = field === 'port_alternator' ? roles.starboard_alternator : field === 'starboard_alternator' ? roles.port_alternator : ''
          return (
            <div key={field} className="grid min-w-0 gap-1.5">
              <FieldLabel htmlFor={`role-${field}`}>{label}</FieldLabel>
              <select
                id={`role-${field}`}
                className="h-9 w-full min-w-0 rounded-md border bg-transparent px-3 text-sm"
                value={value}
                onChange={(e) => setRole({ [field]: e.target.value })}
              >
                <option value="">Not set</option>
                {options.map((item) => (
                  <option key={item.id} value={item.id} disabled={otherAlternator !== '' && item.id === otherAlternator}>
                    {item.name} · {item.signalk_source}
                  </option>
                ))}
                {missing && <option value={value}>Item no longer assigned</option>}
              </select>
            </div>
          )
        })}

        <div className="grid min-w-0 gap-1.5">
          <FieldLabel htmlFor="role-generator_ac_instance">Generator AC input</FieldLabel>
          <select
            id="role-generator_ac_instance"
            className="h-9 w-full min-w-0 rounded-md border bg-transparent px-3 text-sm tabular-nums"
            value={roles.generator_ac_instance}
            onChange={(e) => setRole({ generator_ac_instance: e.target.value })}
          >
            <option value="">Not set</option>
            {acRows.map((row) => (
              <option key={row.instance} value={row.instance}>
                {row.instance} · {row.metrics.find((m) => m.label === 'Real power')?.text ?? '--'}
              </option>
            ))}
            {acMissing && <option value={roles.generator_ac_instance}>{roles.generator_ac_instance} · not reporting</option>}
          </select>
        </div>
      </div>

      <div className="flex flex-col gap-2">
        {detectorLine('Frozen/impossible sensor check', discovery.detectors.frozen)}
        {detectorLine('Engine differential check', discovery.detectors.engines)}
        {detectorLine('Full-bank charging check', discovery.detectors.battery)}
      </div>
    </FormSection>
  )
}
