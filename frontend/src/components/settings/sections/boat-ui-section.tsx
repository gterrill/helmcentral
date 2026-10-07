import { useMemo, useState } from 'react'
import { toast } from 'sonner'

import { FormRow, FormSection, SettingsLayout } from '@/components/patterns'
import { EngineProfileDialog } from '@/components/engine-profile-dialog'
import { VesselParticularsForm } from '@/components/settings/sections/vessel-particulars-form'
import { EquipmentProfileLinker } from '@/components/settings/sections/equipment-profile-linker'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { InputGroup, InputGroupAddon, InputGroupInput, InputGroupText } from '@/components/ui/input-group'
import { useSettingsFormContext } from '@/components/settings/settings-form-context'
import type { RegularSettingsDraft, VesselDraft, VesselEngineRowDraft } from '@/components/settings/settings-draft'
import { readErrorMessage } from '@/lib/api-error'
import { useDashboardPages, type DashboardPage } from '@/hooks/use-dashboard-pages'
import { useVesselCandidates } from '@/hooks/use-vessel-candidates'
import { newGaugeGroupWidgetId, type GaugeWidgetConfig } from '@/lib/dashboard-widgets'
import { titleCaseInstance, type VesselBatteryCandidate, type VesselHouseBankSetting } from '@/lib/vessel-settings'

interface BoatUiSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

export function BoatUiSection({ draft, onChange }: BoatUiSectionProps) {
  return (
    <SettingsLayout title="Vessel">
      <FormSection title="Identity">
        <FormRow>
          <Field>
            <FieldLabel htmlFor="vessel-prefix">Vessel Prefix</FieldLabel>
            <Input
              id="vessel-prefix"
              value={draft.vesselPrefix}
              onChange={(e) => onChange({ vesselPrefix: e.target.value })}
              aria-label="Vessel prefix"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="boat-model">Boat Model</FieldLabel>
            <Input
              id="boat-model"
              value={draft.boatModel}
              onChange={(e) => onChange({ boatModel: e.target.value })}
              aria-label="Boat model"
            />
          </Field>
        </FormRow>
      </FormSection>

      <VesselParticularsForm />

      <VesselEnginesAndPowerSection vessel={draft.vessel} onChange={onChange} />
    </SettingsLayout>
  )
}

// --- Anomaly detection: Engines + Power -------------------------------------

function detectorLine(label: string, status: { ready: boolean; missing?: string } | undefined) {
  const ready = status?.ready === true
  return (
    <div className="flex min-w-0 items-center gap-2 rounded-md border border-border/60 bg-background/40 px-2.5 py-1.5">
      <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${ready ? 'bg-emerald-500' : 'bg-muted-foreground/40'}`} />
      <span className="min-w-0 truncate text-xs text-muted-foreground">
        <span className="font-medium text-foreground">{label}</span>
        {': '}
        {ready ? 'Ready' : `Not set up${status?.missing ? ` — ${status.missing}` : ''}`}
      </span>
    </div>
  )
}

interface VesselEnginesAndPowerSectionProps {
  vessel: VesselDraft | null
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

/**
 * Settings -> Vessel: which engines and which house bank the anomaly
 * detectors (sensor health, full-bank charging, engine differentials)
 * watch. The choices are part of the settings draft and save with the page's
 * Save bar; the live candidates beside them are a separate read. "Apply gauge
 * zones" is the exception: it adds a tile to a dashboard page straight away,
 * as its own action, and needs nothing from the draft but the linked item's
 * profile, which the linker has already written to the inventory item.
 */
function VesselEnginesAndPowerSection({ vessel, onChange }: VesselEnginesAndPowerSectionProps) {
  const { settings: savedSettings, loading: settingsLoading } = useSettingsFormContext()
  // The detector lines are computed from the saved block, so a save landing
  // (which replaces the shared settings) is what re-reads them.
  const { candidates, loading, error } = useVesselCandidates(savedSettings.vessel)
  const { pages, updatePage } = useDashboardPages()

  const engineRows = useMemo(() => vessel?.engineRows ?? {}, [vessel])
  const houseBank = vessel?.houseBank ?? null
  const setHouseBank = (next: VesselHouseBankSetting | null) => {
    if (vessel === null) return
    onChange({ vessel: { ...vessel, houseBank: next } })
  }

  // Every instance the boat is currently publishing, plus every instance
  // already configured (even one that has gone quiet since) - an operator
  // must still be able to see and edit a row for an engine that just isn't
  // running right now.
  const instances = useMemo(() => {
    const set = new Set<string>()
    for (const c of candidates.engines) set.add(c.instance)
    for (const instance of Object.keys(engineRows)) set.add(instance)
    return [...set].sort()
  }, [candidates.engines, engineRows])

  const candidateByInstance = useMemo(
    () => new Map(candidates.engines.map((c) => [c.instance, c])),
    [candidates.engines],
  )

  // Every battery instance the boat is publishing, plus every one the
  // operator has already named (even one that has gone quiet since).
  const batteryInstances = useMemo(() => {
    const byInstance = new Map<string, VesselBatteryCandidate>()
    for (const b of candidates.batteries) byInstance.set(b.instance, b)
    for (const instance of Object.keys(vessel?.batteryNames ?? {})) {
      if (!byInstance.has(instance)) byInstance.set(instance, { path: `electrical.batteries.${instance}`, instance, bus_name: '', solar_charger: false, voltage: null, current: null, soc: null })
    }
    return [...byInstance.values()].sort((a, b) => a.instance.localeCompare(b.instance, undefined, { numeric: true }))
  }, [candidates.batteries, vessel])

  const setRow = (instance: string, patch: Partial<VesselEngineRowDraft>) => {
    if (vessel === null) return
    onChange({
      vessel: {
        ...vessel,
        engineRows: {
          ...vessel.engineRows,
          [instance]: {
            name: vessel.engineRows[instance]?.name ?? titleCaseInstance(instance),
            equipmentId: vessel.engineRows[instance]?.equipmentId ?? '',
            included: vessel.engineRows[instance]?.included ?? false,
            ...patch,
          },
        },
      },
    })
  }

  // "Apply gauge zones" (the plan: reuse EngineProfileDialog, instance
  // already known) - one dialog instance shared by every row, the same
  // App.tsx pattern, opened with a profile/instance pair instead of asking.
  const [applyDialog, setApplyDialog] = useState<{ instance: string; profileId: string } | null>(null)
  const handleApplyGaugeZones = async (title: string, gauges: GaugeWidgetConfig[], _suffixes: string[], hero?: number) => {
    setApplyDialog(null)
    // Lands on the first dashboard page - Settings has no notion of "the
    // active page" the way the dashboard itself does, so this is a
    // deliberate simplification: move the tile afterward from the
    // dashboard if it belongs somewhere else.
    const targetId = pages[0]?.id
    if (!targetId) return

    // The local `pages` copy can be stale the moment a second apply
    // follows a first one in the same visit (each apply is its own round
    // trip, and React may not have re-rendered with the previous save's
    // result yet) - re-reading the page fresh right before building the
    // patch is what makes applying port's zones and then starboard's keep
    // both tiles, rather than the second overwrite the first's.
    let current: DashboardPage
    try {
      const res = await fetch(`/api/dashboard-pages/${targetId}`)
      if (!res.ok) {
        toast.error('Could not apply gauge zones', { description: await readErrorMessage(res) })
        return
      }
      current = (await res.json()) as DashboardPage
    } catch (err) {
      toast.error('Could not apply gauge zones', { description: err instanceof Error ? err.message : 'Request failed' })
      return
    }

    const maxY = current.widgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    const saved = await updatePage(targetId, {
      widgets: [...current.widgets, {
        id: newGaugeGroupWidgetId(current.widgets),
        x: 0, y: maxY, w: 4, h: 7,
        gaugeGroup: { title, gauges, ...(hero === undefined ? {} : { hero }) },
      }],
    })
    // updatePage already toasts on failure (use-dashboard-pages.ts); only
    // the success case needs its own feedback here.
    if (saved) toast.success(`Added "${title}" tile`)
  }

  if (settingsLoading || (loading && vessel !== null)) {
    return (
      <FormSection title="Vessel: Engines and House Bank">
        <p className="text-sm text-muted-foreground">Loading…</p>
      </FormSection>
    )
  }

  // The server named a problem with the stored block. The draft stays null,
  // so a save leaves the block alone, and the operator is told why. Only this
  // explicit error raises the alert: a null draft with no error is just the
  // page hydrating a render after settings arrive.
  if (vessel === null) {
    return (
      <FormSection title="Vessel: Engines and House Bank">
        {savedSettings.vessel_error ? (
          <p role="alert" className="text-sm text-destructive">
            Engines and house bank could not be read: {savedSettings.vessel_error}. They are not shown and will not be changed by a save.
          </p>
        ) : (
          <p className="text-sm text-muted-foreground">Loading…</p>
        )}
      </FormSection>
    )
  }

  return (
    <>
      <FormSection
        title="Engines"
        description="Tick every engine the frozen-sensor and engine-differential checks should watch. Each needs a linked inventory item so its equipment profile applies gauge zones and pack thresholds."
      >
        <div className="flex flex-col gap-2">
          {instances.length === 0 && (
            <p className="text-sm text-muted-foreground">No propulsion instances published yet.</p>
          )}
          {instances.map((instance) => {
            const row: VesselEngineRowDraft = engineRows[instance] ?? { name: titleCaseInstance(instance), equipmentId: '', included: false }
            const live = candidateByInstance.get(instance)
            return (
              <EngineRow
                key={instance}
                instance={instance}
                row={row}
                rpm={live?.rpm ?? null}
                coolantC={live?.coolant_c ?? null}
                onToggle={(included) => setRow(instance, { included })}
                onNameChange={(name) => setRow(instance, { name })}
                onEquipmentIdChange={(equipmentId) => setRow(instance, { equipmentId })}
                onApplyGaugeZones={(profileId) => setApplyDialog({ instance, profileId })}
              />
            )
          })}
        </div>
        {detectorLine('Frozen/impossible sensor check', candidates.detectors.frozen)}
      </FormSection>

      <FormSection
        title="Power"
        description="Pick which battery bank is the house bank the full-bank-charging check watches. Live voltage, current and state of charge are shown for every bank so you can tell which one is which."
      >
        <PowerFieldset
          houseBank={houseBank}
          onChange={setHouseBank}
          batteries={candidates.batteries}
        />
        {detectorLine('Full-bank charging check', candidates.detectors.battery)}
        {detectorLine('Engine differential check', candidates.detectors.engines)}
      </FormSection>

      <FormSection
        title="Battery names"
        description="Name each battery the way you talk about it. Alarms and Mate use your name; leave it empty to use what the boat's instruments call it. Solar charger inputs are not batteries and are not range-checked."
      >
        <div className="flex flex-col gap-2">
          {batteryInstances.length === 0 && (
            <p className="text-sm text-muted-foreground">No batteries published yet.</p>
          )}
          {batteryInstances.map((b) => (
            <BatteryNameRow
              key={b.instance}
              candidate={b}
              name={vessel.batteryNames[b.instance] ?? ''}
              onNameChange={(name) => onChange({ vessel: { ...vessel, batteryNames: { ...vessel.batteryNames, [b.instance]: name } } })}
            />
          ))}
        </div>
      </FormSection>

      {error && <p className="text-xs text-destructive" role="alert">{error}</p>}

      <EngineProfileDialog
        open={applyDialog !== null}
        onCancel={() => setApplyDialog(null)}
        onApply={handleApplyGaugeZones}
        applyLabel="Add tile"
        initialProfileId={applyDialog?.profileId}
        initialInstance={applyDialog ? `propulsion.${applyDialog.instance}` : undefined}
      />
    </>
  )
}

interface EngineRowProps {
  instance: string
  row: VesselEngineRowDraft
  rpm: number | null
  coolantC: number | null
  onToggle: (included: boolean) => void
  onNameChange: (name: string) => void
  onEquipmentIdChange: (id: string) => void
  onApplyGaugeZones: (profileId: string) => void
}

function EngineRow({ instance, row, rpm, coolantC, onToggle, onNameChange, onEquipmentIdChange, onApplyGaugeZones }: EngineRowProps) {
  const [linkedProfileId, setLinkedProfileId] = useState('')

  return (
    <div className="grid min-w-0 grid-cols-1 items-start gap-2 rounded-md border border-border/60 bg-background/40 p-3 sm:grid-cols-[auto_1fr_1fr_auto]">
      <div className="flex items-center gap-2 pt-1.5">
        <Checkbox
          id={`engine-include-${instance}`}
          checked={row.included}
          onCheckedChange={(checked) => onToggle(checked === true)}
          aria-label={`Include ${instance} in anomaly detection`}
        />
        <FieldLabel htmlFor={`engine-include-${instance}`} className="text-xs text-muted-foreground">
          propulsion.{instance}
        </FieldLabel>
      </div>

      <div className="min-w-0">
        <Input
          value={row.name}
          onChange={(e) => onNameChange(e.target.value)}
          placeholder={titleCaseInstance(instance)}
          className="h-8 text-sm"
          aria-label={`Display name for ${instance}`}
        />
        <p className="mt-1 truncate text-xs text-muted-foreground tabular-nums">
          {rpm === null ? '—' : `${Math.round(rpm)} rpm`} · {coolantC === null ? '—' : `${coolantC.toFixed(0)}°C`}
        </p>
      </div>

      <EquipmentProfileLinker
        equipmentId={row.equipmentId}
        onEquipmentIdChange={onEquipmentIdChange}
        profileKind="engine"
        defaultSystem="propulsion"
        namePlaceholder={`${titleCaseInstance(instance)} engine`}
        onLinkedItemChange={(item) => setLinkedProfileId(item?.profile_id ?? '')}
      />

      <div className="flex items-start pt-0.5">
        {linkedProfileId && (
          <Button type="button" size="sm" variant="outline" onClick={() => onApplyGaugeZones(linkedProfileId)}>
            Apply gauge zones
          </Button>
        )}
      </div>
    </div>
  )
}

interface BatteryNameRowProps {
  candidate: VesselBatteryCandidate
  name: string
  onNameChange: (name: string) => void
}

function BatteryNameRow({ candidate, name, onNameChange }: BatteryNameRowProps) {
  const reading = [
    candidate.voltage === null ? null : `${candidate.voltage.toFixed(1)} V`,
    candidate.current === null ? null : `${candidate.current.toFixed(1)} A`,
    candidate.soc === null ? null : `${(candidate.soc * 100).toFixed(0)}%`,
  ].filter((part): part is string => part !== null).join(' · ')

  return (
    <div className="grid min-w-0 grid-cols-1 items-start gap-2 rounded-md border border-border/60 bg-background/40 p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
      <div className="min-w-0">
        <Input
          value={name}
          onChange={(e) => onNameChange(e.target.value)}
          placeholder={candidate.bus_name || `Battery ${candidate.instance}`}
          className="h-8 text-sm"
          aria-label={`Name for battery ${candidate.instance}`}
        />
        <p className="mt-1 truncate text-xs text-muted-foreground">
          {candidate.bus_name ? `The boat calls it ${candidate.bus_name}` : 'The boat gives it no name'}
        </p>
      </div>
      <div className="min-w-0 pt-1.5">
        <p className="truncate text-xs text-muted-foreground tabular-nums">
          Instance {candidate.instance}{reading === '' ? '' : ` · ${reading}`}
        </p>
        {candidate.solar_charger && (
          <p className="truncate text-xs text-muted-foreground">Solar charger input, not a battery</p>
        )}
      </div>
    </div>
  )
}

interface PowerFieldsetProps {
  houseBank: VesselHouseBankSetting | null
  onChange: (next: VesselHouseBankSetting | null) => void
  batteries: { path: string; voltage: number | null; current: number | null; soc: number | null }[]
}

function PowerFieldset({ houseBank, onChange, batteries }: PowerFieldsetProps) {
  const selectedPath = houseBank?.path ?? ''

  const handleBankPick = (path: string) => {
    if (path === '') { onChange(null); return }
    onChange({
      path,
      equipment_id: houseBank?.path === path ? houseBank.equipment_id : '',
      capacity_ah: houseBank?.path === path ? houseBank.capacity_ah : 0,
      cells: houseBank?.path === path ? houseBank.cells : 0,
      warn_voltage: houseBank?.path === path ? houseBank.warn_voltage : undefined,
      high_voltage: houseBank?.path === path ? houseBank.high_voltage : undefined,
    })
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="grid gap-1.5">
        <FieldLabel htmlFor="house-bank-picker">House bank</FieldLabel>
        <select
          id="house-bank-picker"
          className="h-9 w-full min-w-0 rounded-md border bg-transparent px-3 text-sm"
          value={selectedPath}
          onChange={(e) => handleBankPick(e.target.value)}
        >
          <option value="">Not set</option>
          {batteries.map((b) => (
            <option key={b.path} value={b.path}>
              {b.path}
              {b.voltage !== null ? ` — ${b.voltage.toFixed(1)} V` : ''}
              {b.current !== null ? `, ${b.current.toFixed(1)} A` : ''}
              {b.soc !== null ? `, ${(b.soc * 100).toFixed(0)}% SoC` : ''}
            </option>
          ))}
        </select>
      </div>

      {houseBank && (
        <>
          <EquipmentProfileLinker
            equipmentId={houseBank.equipment_id}
            onEquipmentIdChange={(id) => onChange({ ...houseBank, equipment_id: id })}
            profileKind="battery"
            defaultSystem="electrical"
            namePlaceholder="House bank"
          />

          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <Field>
              <FieldLabel htmlFor="house-bank-cells">Cells</FieldLabel>
              <Input
                id="house-bank-cells"
                inputMode="numeric"
                value={houseBank.cells || ''}
                onChange={(e) => onChange({ ...houseBank, cells: Number.parseInt(e.target.value, 10) || 0 })}
                aria-label="House bank cell count"
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="house-bank-capacity">Capacity</FieldLabel>
              <InputGroup>
                <InputGroupInput
                  id="house-bank-capacity"
                  inputMode="decimal"
                  value={houseBank.capacity_ah || ''}
                  onChange={(e) => onChange({ ...houseBank, capacity_ah: Number.parseFloat(e.target.value) || 0 })}
                  aria-label="House bank capacity in amp-hours"
                />
                <InputGroupAddon align="inline-end">
                  <InputGroupText>Ah</InputGroupText>
                </InputGroupAddon>
              </InputGroup>
            </Field>
            <Field>
              <FieldLabel htmlFor="house-bank-warn">Warn override</FieldLabel>
              <InputGroup>
                <InputGroupInput
                  id="house-bank-warn"
                  inputMode="decimal"
                  value={houseBank.warn_voltage ?? ''}
                  placeholder="profile"
                  onChange={(e) => onChange({ ...houseBank, warn_voltage: e.target.value === '' ? undefined : Number.parseFloat(e.target.value) })}
                  aria-label="Pack warn voltage override"
                />
                <InputGroupAddon align="inline-end">
                  <InputGroupText>V</InputGroupText>
                </InputGroupAddon>
              </InputGroup>
            </Field>
            <Field>
              <FieldLabel htmlFor="house-bank-high">High override</FieldLabel>
              <InputGroup>
                <InputGroupInput
                  id="house-bank-high"
                  inputMode="decimal"
                  value={houseBank.high_voltage ?? ''}
                  placeholder="profile"
                  onChange={(e) => onChange({ ...houseBank, high_voltage: e.target.value === '' ? undefined : Number.parseFloat(e.target.value) })}
                  aria-label="Pack high voltage override"
                />
                <InputGroupAddon align="inline-end">
                  <InputGroupText>V</InputGroupText>
                </InputGroupAddon>
              </InputGroup>
            </Field>
          </div>
        </>
      )}
    </div>
  )
}
