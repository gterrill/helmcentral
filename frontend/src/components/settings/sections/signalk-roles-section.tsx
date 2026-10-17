import { useMemo, useRef, useState } from 'react'

import { FormSection } from '@/components/patterns'
import { FieldLabel } from '@/components/ui/field'
import { useSettingsFormContext } from '@/components/settings/settings-form-context'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'
import { BLANK_DRAFT, createEquipment, useEquipment, type EquipmentItem } from '@/hooks/use-inventory'
import { useVesselDiscovery } from '@/hooks/use-vessel-discovery'
import {
  OPTIONAL_ROLE_FIELDS,
  ROLE_NONE,
  SOURCE_KIND_LABELS,
  discoveryRows,
  systemForKind,
  type DiscoveryRow,
  type SourceKind,
  type VesselDetectorStatus,
  type VesselRoles,
} from '@/lib/vessel-settings'

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

/** An option value that means "make an item from this source", never a stored role. */
const SOURCE_PREFIX = 'source:'

/** "PORT START BATT · electrical.batteries.0 · 28.24 V · 100%": the boat's own name when it gives one, the source, and whatever readings discovery has. */
function sourceOptionLabel(row: DiscoveryRow): string {
  return [row.bus_name, row.source, ...row.metrics.map((m) => m.text)].filter((part) => part !== '').join(' · ')
}

/** Sources the boat is reporting for a kind that no item owns yet. A charger's own battery input is not a battery. */
function offerableSources(rows: DiscoveryRow[], kind: SourceKind): DiscoveryRow[] {
  return rows.filter((r) => r.reporting && r.item === null && !(kind === 'battery' && r.charger_input))
}

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
  const { discovery, loading: discoveryLoading } = useVesselDiscovery()
  const roles = draft.roles
  // The latest roles, so a choice that waits on creating an item lands on what
  // the draft holds when the item exists, not on what it held when it started.
  const rolesRef = useRef(roles)
  rolesRef.current = roles
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [busyField, setBusyField] = useState<string | null>(null)

  // Items made here, held until the equipment list has read them back, so a
  // role set to a new item never shows "Item no longer assigned" in between.
  const [madeItems, setMadeItems] = useState<EquipmentItem[]>([])

  const itemsByKind = useMemo(() => {
    const byKind = new Map<string, EquipmentItem[]>()
    const known = new Set(items.map((i) => i.id))
    for (const item of [...items, ...madeItems.filter((m) => !known.has(m.id))]) {
      if (item.source_kind === '') continue
      byKind.set(item.source_kind, [...(byKind.get(item.source_kind) ?? []), item])
    }
    for (const list of byKind.values()) list.sort((a, b) => a.name.localeCompare(b.name))
    return byKind
  }, [items, madeItems])

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

  const setRole = (patch: Partial<VesselRoles>) => onChange({ roles: { ...(rolesRef.current ?? roles), ...patch } })

  // Choosing a source the boat reports makes the item for it first: an item is
  // what a role holds. If that fails the role stays as it was and the error is
  // shown, so there is never a role without its item.
  const choose = async (field: Exclude<keyof VesselRoles, 'generator_ac_instance'>, label: string, kind: SourceKind, value: string) => {
    setErrors((prev) => ({ ...prev, [field]: '' }))
    if (!value.startsWith(SOURCE_PREFIX)) {
      // No generator means no generator AC input: drop the old choice so a
      // stale value is not saved behind the disabled select.
      setRole(field === 'generator' && value === ROLE_NONE ? { generator: value, generator_ac_instance: '' } : { [field]: value })
      return
    }
    setBusyField(field)
    try {
      const created = await createEquipment({
        ...BLANK_DRAFT,
        name: label,
        system: systemForKind(kind),
        signalk_source: value.slice(SOURCE_PREFIX.length),
      })
      setMadeItems((prev) => [...prev, { ...created, source_kind: kind }])
      setRole({ [field]: created.id })
    } catch (err) {
      setErrors((prev) => ({ ...prev, [field]: err instanceof Error ? err.message : 'Could not create the item' }))
    } finally {
      setBusyField(null)
    }
  }

  const emptyLine = (kind: SourceKind | 'ac'): string | null => {
    if (discoveryLoading) return null
    if (!discovery.connected) return "SignalK isn't connected."
    const noun = kind === 'ac' ? 'AC inputs' : SOURCE_KIND_LABELS[kind].toLowerCase()
    return `The boat isn't reporting any ${noun}.`
  }
  const acRows = discoveryRows(discovery, 'ac')
  const noGenerator = roles.generator === ROLE_NONE
  const acMissing = roles.generator_ac_instance !== '' && roles.generator_ac_instance !== ROLE_NONE && !acRows.some((r) => r.instance === roles.generator_ac_instance)

  return (
    <FormSection
      title="Roles"
      description="Pick which equipment item plays each part. The Battery & Power tile, its dawn projection and state-of-charge bands, the Alternators tile, the Generator tile and the anomaly checks read these. Each list holds the items assigned to that kind of source, then the sources the boat is reporting: choosing one makes the item for it. Choosing a source from the boat creates its item straight away; the role itself is saved with Save. Anything left as Not set shows no data; This boat has none answers the question and shows no data either."
    >
      <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2">
        {ITEM_ROLES.map(({ field, label, kind }) => {
          const options = itemsByKind.get(kind) ?? []
          const sources = offerableSources(discoveryRows(discovery, kind), kind)
          const value = roles[field]
          // A saved choice whose item has lost its source, or been deleted,
          // stays visible so the operator sees what is stored instead of a
          // silently blank choice.
          const missing = value !== '' && value !== ROLE_NONE && !options.some((item) => item.id === value)
          const otherAlternator = field === 'port_alternator' ? roles.starboard_alternator : field === 'starboard_alternator' ? roles.port_alternator : ''
          const error = errors[field]
          const empty = options.length === 0 && sources.length === 0 ? emptyLine(kind) : null
          return (
            <div key={field} className="grid min-w-0 gap-1.5">
              <FieldLabel htmlFor={`role-${field}`}>{label}</FieldLabel>
              <select
                id={`role-${field}`}
                className="h-9 w-full min-w-0 rounded-md border bg-transparent px-3 text-sm"
                value={value}
                disabled={busyField === field}
                onChange={(e) => void choose(field, label, kind, e.target.value)}
              >
                <option value="">Not set</option>
                {OPTIONAL_ROLE_FIELDS.includes(field) && <option value={ROLE_NONE}>This boat has none</option>}
                {options.map((item) => (
                  <option key={item.id} value={item.id} disabled={otherAlternator !== '' && item.id === otherAlternator}>
                    {item.name} · {item.signalk_source}
                  </option>
                ))}
                {missing && <option value={value}>Item no longer assigned</option>}
                {sources.length > 0 && (
                  <optgroup label="From the boat">
                    {sources.map((row) => (
                      <option key={row.source} value={SOURCE_PREFIX + row.source}>{sourceOptionLabel(row)}</option>
                    ))}
                  </optgroup>
                )}
              </select>
              {empty && <p className="text-xs text-muted-foreground">{empty}</p>}
              {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
            </div>
          )
        })}

        <div className="grid min-w-0 gap-1.5">
          <FieldLabel htmlFor="role-generator_ac_instance">Generator AC input</FieldLabel>
          <select
            id="role-generator_ac_instance"
            className="h-9 w-full min-w-0 rounded-md border bg-transparent px-3 text-sm tabular-nums"
            value={roles.generator_ac_instance}
            disabled={noGenerator}
            onChange={(e) => setRole({ generator_ac_instance: e.target.value })}
          >
            <option value="">Not set</option>
            <option value={ROLE_NONE}>This boat has none</option>
            {acRows.map((row) => (
              <option key={row.instance} value={row.instance}>
                {row.instance} · {row.metrics.find((m) => m.label === 'Real power')?.text ?? '--'}
              </option>
            ))}
            {acMissing && <option value={roles.generator_ac_instance}>{roles.generator_ac_instance} · not reporting</option>}
          </select>
          {noGenerator && <p className="text-xs text-muted-foreground">Not needed: this boat has no generator.</p>}
          {!noGenerator && acRows.length === 0 && !acMissing && emptyLine('ac') && <p className="text-xs text-muted-foreground">{emptyLine('ac')}</p>}
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
