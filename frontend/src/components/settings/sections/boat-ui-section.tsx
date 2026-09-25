import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'

import { EngineProfileDialog } from '@/components/engine-profile-dialog'
import { EquipmentProfileLinker } from '@/components/settings/sections/equipment-profile-linker'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { InputGroup, InputGroupAddon, InputGroupInput, InputGroupText } from '@/components/ui/input-group'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'
import { readErrorMessage } from '@/lib/api-error'
import { useDashboardPages, type DashboardPage } from '@/hooks/use-dashboard-pages'
import { useVesselSettings } from '@/hooks/use-vessel-settings'
import { newGaugeGroupWidgetId, type GaugeWidgetConfig } from '@/lib/dashboard-widgets'
import { titleCaseInstance, type VesselEngineSetting, type VesselHouseBankSetting } from '@/lib/vessel-settings'

interface BoatUiSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

export function BoatUiSection({ draft, onChange }: BoatUiSectionProps) {
  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">Vessel</FieldLegend>
        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
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
        </div>
      </FieldSet>

      <VesselEnginesAndPowerSection />
    </div>
  )
}

// --- Anomaly detection: Engines + Power -------------------------------------

/** One editable engine row's state, keyed by SignalK propulsion instance. */
interface EngineRowDraft {
  name: string
  equipmentId: string
  included: boolean
}

function detectorLine(label: string, status: { ready: boolean; missing?: string } | undefined) {
  const ready = status?.ready === true
  return (
    <div className="flex min-w-0 items-center gap-2 rounded-md border border-border/60 bg-background/40 px-2.5 py-1.5">
      <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${ready ? 'bg-emerald-500' : 'bg-muted-foreground/40'}`} />
      <span className="min-w-0 truncate text-[11px] text-muted-foreground">
        <span className="font-medium text-foreground">{label}</span>
        {': '}
        {ready ? 'Ready' : `Not set up${status?.missing ? ` — ${status.missing}` : ''}`}
      </span>
    </div>
  )
}

/**
 * Settings -> Vessel: which engines and which house bank the anomaly
 * detectors (sensor health, full-bank charging, engine differentials)
 * watch. Its own save button and its own API (GET/POST /api/vessel) -
 * separate from the pinned "Save Settings" button above, the same "owned
 * exclusively by its own immediate save()" pattern the Widgets provider
 * cards already use (see settings-draft.ts's own comment on that split).
 */
function VesselEnginesAndPowerSection() {
  const { settings, candidates, loading, error, saving, save } = useVesselSettings()
  const { pages, updatePage } = useDashboardPages()

  const [engineRows, setEngineRows] = useState<Record<string, EngineRowDraft>>({})
  const [houseBank, setHouseBank] = useState<VesselHouseBankSetting | null>(null)
  const [hydrated, setHydrated] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  // Seeds the editable draft from the server exactly once, the moment the
  // first fetch lands - not on every settings change, or the operator's own
  // in-progress edits would be clobbered by the next background refresh
  // (the save button itself triggers one, see useVesselSettings.save).
  useEffect(() => {
    if (hydrated || loading) return
    setHydrated(true)
    const rows: Record<string, EngineRowDraft> = {}
    for (const e of settings.engines) {
      rows[e.instance] = { name: e.name || titleCaseInstance(e.instance), equipmentId: e.equipment_id, included: true }
    }
    setEngineRows(rows)
    setHouseBank(settings.house_bank)
  }, [hydrated, loading, settings])

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

  const setRow = (instance: string, patch: Partial<EngineRowDraft>) => {
    setEngineRows((prev) => ({
      ...prev,
      [instance]: {
        name: prev[instance]?.name ?? titleCaseInstance(instance),
        equipmentId: prev[instance]?.equipmentId ?? '',
        included: prev[instance]?.included ?? false,
        ...patch,
      },
    }))
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

  const handleSave = async () => {
    setSaveError(null)
    setSaved(false)
    const engines: VesselEngineSetting[] = instances
      .filter((instance) => engineRows[instance]?.included)
      .map((instance) => ({
        instance,
        name: engineRows[instance].name.trim() || titleCaseInstance(instance),
        equipment_id: engineRows[instance].equipmentId,
      }))
    try {
      await save({ engines, house_bank: houseBank })
      setSaved(true)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    }
  }

  if (loading && !hydrated) {
    return (
      <FieldSet>
        <FieldLegend variant="label">Vessel: Engines and House Bank</FieldLegend>
        <p className="mt-3 text-sm text-muted-foreground">Loading…</p>
      </FieldSet>
    )
  }

  return (
    <>
      <FieldSet>
        <FieldLegend variant="label">Engines</FieldLegend>
        <p className="mt-1 text-[11px] text-muted-foreground">
          Tick every engine the frozen-sensor and engine-differential checks should watch. Each needs a linked
          inventory item so its equipment profile applies gauge zones and pack thresholds.
        </p>
        <div className="mt-3 flex flex-col gap-2">
          {instances.length === 0 && (
            <p className="text-sm text-muted-foreground">No propulsion instances published yet.</p>
          )}
          {instances.map((instance) => {
            const row = engineRows[instance] ?? { name: titleCaseInstance(instance), equipmentId: '', included: false }
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
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">Power</FieldLegend>
        <p className="mt-1 text-[11px] text-muted-foreground">
          Pick which battery bank is the house bank the full-bank-charging check watches. Live voltage, current and
          state of charge are shown for every bank so you can tell which one is which.
        </p>
        <div className="mt-3">
          <PowerFieldset
            houseBank={houseBank}
            onChange={setHouseBank}
            batteries={candidates.batteries}
          />
        </div>
        {detectorLine('Full-bank charging check', candidates.detectors.battery)}
        {detectorLine('Engine differential check', candidates.detectors.engines)}
      </FieldSet>

      <div className="flex flex-wrap items-center gap-3">
        <Button type="button" onClick={() => void handleSave()} disabled={saving}>
          {saving ? 'Saving…' : 'Save Vessel Settings'}
        </Button>
        {saved && !saveError && <span className="text-[11px] text-muted-foreground">Saved.</span>}
        {(saveError ?? error) && (
          <span className="text-[11px] text-destructive" role="alert">{saveError ?? error}</span>
        )}
      </div>

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
  row: EngineRowDraft
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
        <p className="mt-1 truncate text-[11px] text-muted-foreground tabular-nums">
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
