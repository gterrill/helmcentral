import { describe, it, expect, vi, beforeEach } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useState } from 'react'
import { emptyVesselElectrical } from '@/lib/vessel-settings'
import { BoatUiSection } from '@/components/settings/sections/boat-ui-section'
import { SettingsFormProvider } from '@/components/settings/settings-form-context'
import { VesselParticularsProvider } from '@/components/settings/vessel-particulars-context'
import {
  buildRegularSettingsPatch,
  hydrateDraftFromSettings,
  type RegularSettingsDraft,
} from '@/components/settings/settings-draft'

// Settings -> Vessel's Engines/Power UI: the fieldsets that turn
// GET /api/vessel/candidates and the settings payload's vessel block into
// something an operator can actually use. The choices live in the page's
// draft and save with the Save bar (settings-vessel-save.test.tsx covers the
// bar); here the draft is read back to see what a save would send. Every dependent fetch (candidates, vessel
// settings, equipment items, equipment profiles, dashboard pages) is routed
// through one mock, the same pattern equipment-editor.test.tsx uses for its
// own multi-endpoint component.

type SavedVessel = { engines: { instance: string; name: string; equipment_id: string }[]; house_bank: unknown; batteries: { instance: string; name: string }[]; alternators: { port_instance: string; starboard_instance: string }; electrical: Record<string, string>; solar: { instance: string; name: string }[] }
let latestDraft: RegularSettingsDraft
// What the next Save would send as `vessel`.
const pendingVessel = () => buildRegularSettingsPatch(latestDraft).vessel as unknown as SavedVessel
const equipmentItems = [
  { id: 'eq-1', name: 'Port engine', category: 'mechanical', system: 'propulsion', manufacturer: '', model: '', serial: '', quantity: 1, part_number: '', required_quantity: null, status: 'deployed', zone_id: null, bin_id: null, zone_name: '', bin_code: '', location_detail: '', install_date: '', hour_meter_path: '', profile_id: '', aliases: [], verified_aboard: false, notes: '', link_count: 0, created_at: '', updated_at: '', photo_ids: [], exclusive_photo_ids: [] },
  { id: 'eq-2', name: 'Starboard engine', category: 'mechanical', system: 'propulsion', manufacturer: '', model: '', serial: '', quantity: 1, part_number: '', required_quantity: null, status: 'deployed', zone_id: null, bin_id: null, zone_name: '', bin_code: '', location_detail: '', install_date: '', hour_meter_path: '', profile_id: '', aliases: [], verified_aboard: false, notes: '', link_count: 0, created_at: '', updated_at: '', photo_ids: [], exclusive_photo_ids: [] },
]
// A single dashboard page, mutated by the mock's PATCH handler exactly like
// the real backend would -- lets "apply gauge zones" tests assert on
// server-side state rather than trusting whatever the component's own
// (possibly stale) local copy says happened.
let dashboardPage: { id: string; widgets: { id: string; x: number; y: number; w: number; h: number; gaugeGroup?: { title: string } }[] }
const profiles = [
  { id: 'engine-profile-1', name: 'Test Engine Profile', kind: 'engine', gauges: [{ path_suffix: 'oilPressure', label: 'Oil', display: 'numeric', quantity: 'pressure', unit: 'psi' }] },
  { id: 'battery-profile-1', name: 'Test Battery Profile', kind: 'battery', chemistry: 'LiFePO4', full_soc: { value: 0.98 }, charge_warn: { value: 3.55 }, charge_high: { value: 3.65 } },
]

let savedPortInstance = ''
let savedEngines: { instance: string; name: string; equipment_id: string }[] = []
let savedElectrical: Record<string, string> = {}
const fetchMock = vi.fn()

function stubFetch() {
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'

    if (u.endsWith('/api/vessel/candidates')) {
      return Promise.resolve({
        ok: true,
        json: async () => ({
          engines: [
            { instance: 'port', rpm: 1800, coolant_c: 76 },
            { instance: 'starboard', rpm: 1810, coolant_c: 75 },
          ],
          batteries: [
            { path: 'electrical.batteries.0', instance: '0', bus_name: 'Batrium-BMS (Victron profile)', charger_input: false, voltage: 27.2, current: 15.6, soc: 0.79 },
            { path: 'electrical.batteries.1', instance: '1', bus_name: 'BlueSolar Charger MPPT 100/50 re', charger_input: true, voltage: 76.7, current: 0, soc: null },
            { path: 'electrical.batteries.512', instance: '512', bus_name: '', charger_input: false, voltage: 27.21, current: 267.6, soc: 0.79 },
          ],
          alternators: [
            { instance: '1', current: null },
            { instance: '2', current: 31.5 },
            { instance: '12', current: 28.5 },
          ],
          generators: [
            { instance: '0', state: 'stopped' },
            { instance: '12', state: null },
          ],
          ac: [{ instance: '5', real_power: 1250 }, { instance: '40', real_power: null }],
          chargers: [{ instance: '5', current: 20, acin_current: 6 }, { instance: '276', current: null, acin_current: null }],
          inverters: [{ instance: '5', ac_out: 420 }, { instance: '276', ac_out: null }],
          solar: [{ instance: '288', panel_power: 110 }, { instance: '289', panel_power: null }],
          detectors: {
            frozen: { ready: false, missing: 'Tick at least one engine' },
            battery: { ready: false, missing: 'Pick your house bank' },
            engines: { ready: false, missing: 'Tick a second engine' },
          },
        }),
      })
    }
    if (u.endsWith('/api/settings')) {
      return Promise.resolve({ ok: true, json: async () => ({ vessel: { engines: [], house_bank: null, batteries: [] } }) })
    }
    if (u.endsWith('/api/vessel/particulars')) {
      return Promise.resolve({ ok: false, status: 503, json: async () => ({ error: 'not under test' }) })
    }
    if (u.match(/\/api\/inventory\/equipment\/[^/?]+$/) && method === 'GET') {
      const id = u.split('/').pop()
      const item = equipmentItems.find((i) => i.id === id)
      return Promise.resolve({ ok: true, json: async () => ({ item }) })
    }
    if (u.match(/\/api\/inventory\/equipment\/[^/?]+$/) && method === 'PUT') {
      const id = u.split('/').pop()
      const body = JSON.parse(String(init?.body))
      const index = equipmentItems.findIndex((i) => i.id === id)
      if (index !== -1) equipmentItems[index] = { ...equipmentItems[index], ...body }
      return Promise.resolve({ ok: true, json: async () => ({ item: equipmentItems[index] }) })
    }
    if (u.includes('/api/inventory/equipment') && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ items: equipmentItems }) })
    }
    if (u.includes('/api/equipment-profiles')) {
      return Promise.resolve({ ok: true, json: async () => ({ profiles, problems: [] }) })
    }
    if (u.endsWith('/api/dashboard-pages')) {
      return Promise.resolve({ ok: true, json: async () => ({ pages: [dashboardPage] }) })
    }
    const pageMatch = u.match(/\/api\/dashboard-pages\/([^/?]+)$/)
    if (pageMatch && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => dashboardPage })
    }
    if (pageMatch && method === 'PATCH') {
      const patch = JSON.parse(String(init?.body)) as Partial<typeof dashboardPage>
      dashboardPage = { ...dashboardPage, ...patch }
      return Promise.resolve({ ok: true, json: async () => dashboardPage })
    }
    if (u.includes('/api/signalk/paths')) {
      return Promise.resolve({ ok: true, json: async () => ({ paths: [] }) })
    }
    return Promise.resolve({ ok: true, json: async () => ({}) })
  })
}

beforeEach(() => {
  dashboardPage = { id: 'page-1', widgets: [] }
  savedPortInstance = ''
  savedEngines = []
  savedElectrical = {}
  fetchMock.mockReset()
  stubFetch()
  vi.stubGlobal('fetch', fetchMock)
})

function SectionHarness() {
  const [draft, setDraft] = useState<RegularSettingsDraft>(() => hydrateDraftFromSettings({ vessel: { engines: savedEngines, house_bank: null, batteries: [], alternators: { port_instance: savedPortInstance, starboard_instance: '' }, electrical: { ...emptyVesselElectrical, ...savedElectrical }, solar: [] } }))
  latestDraft = draft
  return <BoatUiSection draft={draft} onChange={(patch) => setDraft((previous) => ({ ...previous, ...patch }))} />
}

function renderSection() {
  return render(
    <SettingsFormProvider>
      <VesselParticularsProvider>
        <SectionHarness />
      </VesselParticularsProvider>
    </SettingsFormProvider>,
  )
}

describe('Settings -> Vessel: Engines and Power', () => {
  it('lists discovered engines with their live rpm and coolant temperature', async () => {
    renderSection()

    await screen.findByText('propulsion.port')
    expect(screen.getByText('propulsion.starboard')).toBeInTheDocument()
    expect(screen.getByText(/1800 rpm/)).toBeInTheDocument()
    expect(screen.getByText(/76°C/)).toBeInTheDocument()
  })

  it('keeps a saved engine the boat has stopped reporting, and says Not reporting', async () => {
    savedEngines = [{ instance: 'wing', name: 'Wing', equipment_id: '' }]
    renderSection()

    await screen.findByText('propulsion.wing')
    expect(screen.getByText('Not reporting')).toBeInTheDocument()
  })

  it('shows both engine checks under Engines, not Power', async () => {
    renderSection()

    const heading = await screen.findByRole('heading', { name: 'Engines' })
    const engines = heading.closest('[data-slot="card"]')
    if (!(engines instanceof HTMLElement)) throw new Error('Engines heading is not inside a card')
    expect(within(engines).getByText(/Tick at least one engine/)).toBeInTheDocument()
    expect(within(engines).getByText(/Tick a second engine/)).toBeInTheDocument()
  })

  it('shows each detector as not set up with what is missing, on a fresh install', async () => {
    renderSection()

    await screen.findByText(/Pick your house bank/)
    expect(screen.getByText(/Tick at least one engine/)).toBeInTheDocument()
    expect(screen.getByText(/Tick a second engine/)).toBeInTheDocument()
  })

  it('ticking engines puts the included instances only into what a save sends', async () => {
    renderSection()

    await screen.findByText('propulsion.port')
    fireEvent.click(screen.getByLabelText('Include port as an engine'))
    fireEvent.click(screen.getByLabelText('Include starboard as an engine'))

    await waitFor(() => expect(pendingVessel().engines).toHaveLength(2))
    expect(pendingVessel().engines.map((e) => e.instance).sort()).toEqual(['port', 'starboard'])

    fireEvent.click(screen.getByLabelText('Include starboard as an engine'))
    await waitFor(() => expect(pendingVessel().engines.map((e) => e.instance)).toEqual(['port']))
  })

  it('lists the discovered alternator instances with live current, and Not set', async () => {
    renderSection()

    const port = await screen.findByLabelText('Port alternator')
    expect(within(port).getByRole('option', { name: 'Not set' })).toBeInTheDocument()
    expect(within(port).getByRole('option', { name: '2 · 31.5 A' })).toBeInTheDocument()
    expect(within(port).getByRole('option', { name: '12 · 28.5 A' })).toBeInTheDocument()
    expect(within(port).getByRole('option', { name: '1 · --' })).toBeInTheDocument()
    expect((port as HTMLSelectElement).value).toBe('')
    expect(pendingVessel().alternators).toEqual({ port_instance: '', starboard_instance: '' })
  })

  it('picking port and starboard carries both instances into what a save sends', async () => {
    renderSection()

    fireEvent.change(await screen.findByLabelText('Port alternator'), { target: { value: '2' } })
    fireEvent.change(screen.getByLabelText('Starboard alternator'), { target: { value: '12' } })

    await waitFor(() => expect(pendingVessel().alternators).toEqual({ port_instance: '2', starboard_instance: '12' }))
    // The instance chosen for one side cannot be chosen for the other.
    expect(within(screen.getByLabelText('Starboard alternator')).getByRole('option', { name: '2 · 31.5 A' })).toBeDisabled()
  })

  it('keeps a saved alternator the boat no longer reports, marked not reporting', async () => {
    savedPortInstance = '7'
    renderSection()

    const port = await screen.findByLabelText('Port alternator')
    expect((port as HTMLSelectElement).value).toBe('7')
    expect(within(port).getByRole('option', { name: '7 · not reporting' })).toBeInTheDocument()
    // A live instance with no current reads "--"; a vanished one says so.
    expect(within(port).getByRole('option', { name: '1 · --' })).toBeInTheDocument()
  })

  it('lists the discovered electrical instances with live readings, and Not set', async () => {
    renderSection()

    const generator = await screen.findByLabelText('Generator')
    expect(within(generator).getByRole('option', { name: 'Not set' })).toBeInTheDocument()
    expect(within(generator).getByRole('option', { name: '0 · stopped' })).toBeInTheDocument()
    expect(within(generator).getByRole('option', { name: '12 · --' })).toBeInTheDocument()
    expect(within(screen.getByLabelText('Generator AC input')).getByRole('option', { name: '5 · 1250 W' })).toBeInTheDocument()
    expect(within(screen.getByLabelText('Shore charger')).getByRole('option', { name: '5 · 20.0 A out · 6.0 A AC in' })).toBeInTheDocument()
    expect(within(screen.getByLabelText('Shore charger')).getByRole('option', { name: '276 · --' })).toBeInTheDocument()
    expect(within(screen.getByLabelText('Inverter')).getByRole('option', { name: '5 · 420 W' })).toBeInTheDocument()
    expect(within(screen.getByLabelText('Voltage readout battery')).getByRole('option', { name: '0 · 27.2 V' })).toBeInTheDocument()
    // A charger input is not a battery and is not offered.
    expect(within(screen.getByLabelText('Voltage readout battery')).queryByRole('option', { name: /^1 ·/ })).toBeNull()
    expect((generator as HTMLSelectElement).value).toBe('')
    expect(pendingVessel().electrical).toEqual({ generator_instance: '', generator_ac_instance: '', shore_charger_instance: '', inverter_instance: '', voltage_readout_battery: '' })
  })

  it('picking the electrical instances carries them into what a save sends', async () => {
    renderSection()

    fireEvent.change(await screen.findByLabelText('Generator'), { target: { value: '0' } })
    fireEvent.change(screen.getByLabelText('Generator AC input'), { target: { value: '5' } })
    fireEvent.change(screen.getByLabelText('Shore charger'), { target: { value: '5' } })
    fireEvent.change(screen.getByLabelText('Inverter'), { target: { value: '5' } })
    fireEvent.change(screen.getByLabelText('Voltage readout battery'), { target: { value: '512' } })

    await waitFor(() => expect(pendingVessel().electrical).toEqual({
      generator_instance: '0', generator_ac_instance: '5', shore_charger_instance: '5', inverter_instance: '5', voltage_readout_battery: '512',
    }))
  })

  it('keeps a saved electrical instance the boat no longer reports, marked not reporting', async () => {
    savedElectrical = { inverter_instance: '9' }
    renderSection()

    const inverter = await screen.findByLabelText('Inverter')
    expect((inverter as HTMLSelectElement).value).toBe('9')
    expect(within(inverter).getByRole('option', { name: '9 · not reporting' })).toBeInTheDocument()
  })

  it('names solar controllers by instance and saves only the named ones', async () => {
    renderSection()

    const first = await screen.findByLabelText('Name for solar controller 288')
    expect(first).toHaveAttribute('placeholder', 'Array 288')
    expect(screen.getByText(/Instance 288 · 110 W/)).toBeInTheDocument()
    fireEvent.change(first, { target: { value: ' Port arch ' } })

    await waitFor(() => expect(pendingVessel().solar).toEqual([{ instance: '288', name: 'Port arch' }]))
    expect(screen.getByLabelText('Name for solar controller 289')).toHaveValue('')
  })

  it('has no Save button of its own: the page Save bar owns it', async () => {
    renderSection()
    await screen.findByText('propulsion.port')
    expect(screen.queryByRole('button', { name: /save vessel settings/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /save particulars/i })).toBeNull()
  })

  it('lists batteries with live voltage, current and SoC in the house bank picker', async () => {
    renderSection()

    const picker = await screen.findByLabelText('House bank')
    expect(within(picker).getByText(/electrical\.batteries\.0.*27\.2 V.*15\.6 A.*79% SoC/)).toBeInTheDocument()
    expect(within(picker).getByText(/electrical\.batteries\.512/)).toBeInTheDocument()
  })

  it('picking a house bank reveals cell count, capacity and threshold override fields', async () => {
    renderSection()

    const picker = await screen.findByLabelText('House bank')
    fireEvent.change(picker, { target: { value: 'electrical.batteries.512' } })

    expect(await screen.findByLabelText('House bank cell count')).toBeInTheDocument()
    expect(screen.getByLabelText('House bank capacity in amp-hours')).toBeInTheDocument()
    expect(screen.getByLabelText('Pack warn voltage override')).toBeInTheDocument()
    expect(screen.getByLabelText('Pack high voltage override')).toBeInTheDocument()
  })

  it('the house bank carries its path, cells and capacity into what a save sends', async () => {
    renderSection()

    const picker = await screen.findByLabelText('House bank')
    fireEvent.change(picker, { target: { value: 'electrical.batteries.512' } })

    fireEvent.change(await screen.findByLabelText('House bank cell count'), { target: { value: '8' } })
    fireEvent.change(screen.getByLabelText('House bank capacity in amp-hours'), { target: { value: '400' } })

    await waitFor(() => expect(pendingVessel().house_bank).toMatchObject({ path: 'electrical.batteries.512', cells: 8, capacity_ah: 400 }))
  })

  it('offers only engine-kind profiles once an engine row is linked to an item', async () => {
    renderSection()

    await screen.findByText('propulsion.port')
    const linkers = screen.getAllByLabelText('Linked equipment item')
    fireEvent.change(linkers[0], { target: { value: 'eq-1' } })

    // The select renders before its profile options load, so wait for the
    // options themselves rather than just the select (flaked on a loaded CI
    // runner at 1030 ms).
    const profileSelect = await screen.findByLabelText('Equipment profile', {}, { timeout: 5000 })
    await waitFor(() => expect(within(profileSelect).getByText('Test Engine Profile')).toBeInTheDocument(), { timeout: 5000 })
    expect(within(profileSelect).queryByText('Test Battery Profile')).toBeNull()
  })

  // "Apply gauge zones" (code review finding 5): applying port's profile,
  // then starboard's, must leave the dashboard page holding both tiles.
  // The bug this catches is a raw PATCH built off a stale, never-refreshed
  // copy of the page's widget list: it would overwrite port's just-added
  // tile the moment starboard applied, since both PATCH bodies would be
  // built from the same original (empty) widgets array. Applying twice
  // through the real UI flow -- and asserting on the mock server's own
  // state after each -- forces that race to show up if it exists.
  it('applying gauge zones to port then starboard keeps both tiles', async () => {
    renderSection()

    await screen.findByText('propulsion.port')
    const linkers = screen.getAllByLabelText('Linked equipment item')
    const applyGaugeZonesFor = async (rowIndex: number, equipmentId: string) => {
      fireEvent.change(linkers[rowIndex], { target: { value: equipmentId } })
      // findAllByLabelText resolves as soon as ONE match exists, which on the
      // second row is too early: port's select from the first call is still
      // there, so a single-match poll can win the race before starboard's
      // own (async fetchEquipment-driven) select has rendered. Wait for the
      // count this row's own linking should produce, not just "at least one".
      await waitFor(() => expect(screen.getAllByLabelText('Equipment profile')).toHaveLength(rowIndex + 1))
      const profileSelects = screen.getAllByLabelText('Equipment profile')
      fireEvent.change(profileSelects[rowIndex], { target: { value: 'engine-profile-1' } })

      await waitFor(() => expect(screen.getAllByRole('button', { name: 'Apply gauge zones' })).toHaveLength(rowIndex + 1))
      const applyButtons = screen.getAllByRole('button', { name: 'Apply gauge zones' })
      fireEvent.click(applyButtons[rowIndex])

      const addTile = await screen.findByRole('button', { name: 'Add tile' })
      await waitFor(() => expect(addTile).not.toBeDisabled())
      fireEvent.click(addTile)
      await waitFor(() => expect(screen.queryByRole('button', { name: 'Add tile' })).toBeNull())
    }

    await applyGaugeZonesFor(0, 'eq-1')
    await waitFor(() => expect(dashboardPage.widgets).toHaveLength(1))

    await applyGaugeZonesFor(1, 'eq-2')
    await waitFor(() => expect(dashboardPage.widgets).toHaveLength(2))

    const titles = dashboardPage.widgets.map((w) => w.gaugeGroup?.title)
    expect(titles).toContain('Port')
    expect(titles).toContain('Starboard')
  })
})

describe('Settings -> Vessel: Battery names', () => {
  it('lists each battery instance with what the boat calls it as the hint', async () => {
    renderSection()

    const bms = await screen.findByLabelText('Name for battery 0')
    expect(bms).toHaveAttribute('placeholder', 'Batrium-BMS (Victron profile)')
    expect(screen.getByText('The boat calls it Batrium-BMS (Victron profile)')).toBeInTheDocument()
    // No bus name at all: a plain placeholder and an honest hint.
    expect(screen.getByLabelText('Name for battery 512')).toHaveAttribute('placeholder', 'Battery 512')
    expect(screen.getByText('The boat gives it no name')).toBeInTheDocument()
  })

  it('marks a charger input as not a battery', async () => {
    renderSection()

    await screen.findByLabelText('Name for battery 1')
    expect(screen.getAllByText('Charger input, not a battery')).toHaveLength(1)
  })

  it('saves a typed name with the Save bar patch and leaves an empty one out', async () => {
    renderSection()

    const input = await screen.findByLabelText('Name for battery 512')
    fireEvent.change(input, { target: { value: 'Port Engine Starter Battery' } })
    fireEvent.change(screen.getByLabelText('Name for battery 0'), { target: { value: '   ' } })

    await waitFor(() => expect(pendingVessel().batteries).toEqual([{ instance: '512', name: 'Port Engine Starter Battery' }]))
  })
})
