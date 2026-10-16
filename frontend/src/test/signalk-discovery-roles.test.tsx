import { describe, it, expect, vi, beforeEach } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useState } from 'react'

import { SignalKDiscoverySection } from '@/components/settings/sections/signalk-discovery-section'
import { SignalKRolesSection } from '@/components/settings/sections/signalk-roles-section'
import { SettingsFormProvider } from '@/components/settings/settings-form-context'
import { buildRegularSettingsPatch, hydrateDraftFromSettings, type RegularSettingsDraft } from '@/components/settings/settings-draft'
import { emptyVesselRoles } from '@/lib/vessel-settings'
import { invalidateQueries } from '@/lib/api-query'

// Settings -> SignalK -> Discovery and Roles. Every dependent fetch (the
// discovery list, equipment items, the settings payload) is routed through one
// mock, the same pattern equipment-editor.test.tsx uses.

function item(overrides: Record<string, unknown>) {
  return {
    id: 'eq', name: 'Item', category: 'general', system: 'other', manufacturer: '', model: '', serial: '', quantity: 1, part_number: '',
    required_quantity: null, status: 'deployed', zone_id: null, bin_id: null, zone_name: '', bin_code: '', location_detail: '', install_date: '',
    signalk_source: '', capacity_ah: 0, cells: 0, warn_voltage: 0, high_voltage: 0, source_kind: '', hour_meter_source: '',
    profile_id: '', aliases: [], verified_aboard: false, notes: '', link_count: 0, created_at: '', updated_at: '', photo_ids: [], exclusive_photo_ids: [],
    ...overrides,
  }
}

let equipmentItems: ReturnType<typeof item>[]
let discoveryBody: Record<string, unknown>
let settingsBody: Record<string, unknown>
let putFailsWith: string | null
const fetchMock = vi.fn()

const row = (source: string, instance: string, metrics: { label: string; text: string }[], extra: Record<string, unknown> = {}) => ({
  source, instance, bus_name: '', charger_input: false, reporting: true, metrics, item: null, ...extra,
})

function stubFetch() {
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u.endsWith('/api/vessel/discovery')) return Promise.resolve({ ok: true, json: async () => discoveryBody })
    if (u.endsWith('/api/settings')) return Promise.resolve({ ok: true, json: async () => settingsBody })
    if (u.match(/\/api\/inventory\/equipment\/[^/?]+$/) && method === 'GET') {
      const found = equipmentItems.find((i) => i.id === u.split('/').pop())
      return Promise.resolve({ ok: true, json: async () => ({ item: found }) })
    }
    if (u.match(/\/api\/inventory\/equipment\/[^/?]+$/) && method === 'PUT') {
      if (putFailsWith) return Promise.resolve({ ok: false, status: 409, json: async () => ({ error: putFailsWith }) })
      const id = u.split('/').pop()
      const body = JSON.parse(String(init?.body))
      const index = equipmentItems.findIndex((i) => i.id === id)
      equipmentItems[index] = { ...equipmentItems[index], ...body }
      return Promise.resolve({ ok: true, json: async () => ({ item: equipmentItems[index] }) })
    }
    if (u.endsWith('/api/inventory/equipment') && method === 'POST') {
      const body = JSON.parse(String(init?.body))
      const created = item({ id: 'eq-new', ...body })
      equipmentItems.push(created)
      return Promise.resolve({ ok: true, status: 201, json: async () => ({ item: created }) })
    }
    if (u.includes('/api/inventory/equipment') && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ items: equipmentItems }) })
    }
    return Promise.resolve({ ok: true, json: async () => ({}) })
  })
}

beforeEach(() => {
  putFailsWith = null
  equipmentItems = [
    item({ id: 'eng-1', name: 'Port engine', system: 'propulsion', signalk_source: 'propulsion.port', source_kind: 'engine' }),
    item({ id: 'bank-1', name: 'Lithium bank', system: 'electrical', signalk_source: 'electrical.batteries.512', source_kind: 'battery' }),
    item({ id: 'bank-2', name: 'Starter battery', system: 'electrical', signalk_source: 'electrical.batteries.3', source_kind: 'battery' }),
    item({ id: 'alt-2', name: 'Port alternator', system: 'electrical', signalk_source: 'electrical.alternator.2', source_kind: 'alternator' }),
    item({ id: 'alt-12', name: 'Starboard alternator', system: 'electrical', signalk_source: 'electrical.alternator.12', source_kind: 'alternator' }),
    item({ id: 'gen-1', name: 'Generator', system: 'electrical', signalk_source: 'electrical.generator.0', source_kind: 'generator' }),
    item({ id: 'spare-1', name: 'Spare impeller' }),
    item({ id: 'eng-2', name: 'Starboard engine', system: 'propulsion' }),
  ]
  discoveryBody = {
    connected: true,
    groups: [
      { kind: 'engine', mappable: true, rows: [
        row('propulsion.port', 'port', [{ label: 'RPM', text: '1800 rpm' }, { label: 'Coolant', text: '77 °C' }], { item: { id: 'eng-1', name: 'Port engine' } }),
        row('propulsion.starboard', 'starboard', [{ label: 'RPM', text: '1810 rpm' }]),
        row('propulsion.wing', 'wing', [], { reporting: false, item: { id: 'eng-9', name: 'Wing engine' } }),
      ] },
      { kind: 'battery', mappable: true, rows: [
        row('electrical.batteries.0', '0', [{ label: 'Voltage', text: '27.20 V' }, { label: 'State of charge', text: '79%' }], { bus_name: 'Batrium-BMS' }),
        row('electrical.batteries.1', '1', [{ label: 'Voltage', text: '76.70 V' }], { bus_name: 'BlueSolar', charger_input: true }),
      ] },
      { kind: 'alternator', mappable: true, rows: [] },
      { kind: 'ac', mappable: false, rows: [row('electrical.ac.5', '5', [{ label: 'Real power', text: '1250 W' }])] },
    ],
    detectors: {
      frozen: { ready: false, missing: 'Assign an engine to a propulsion source' },
      battery: { ready: false, missing: 'Choose your house bank under Roles' },
      engines: { ready: false, missing: 'Assign a second engine' },
    },
  }
  settingsBody = { roles: { ...emptyVesselRoles } }
  fetchMock.mockReset()
  stubFetch()
  vi.stubGlobal('fetch', fetchMock)
  void invalidateQueries(['/api/vessel/discovery'])
})

let latestDraft: RegularSettingsDraft

function RolesHarness({ roles = settingsBody.roles }: { roles?: unknown }) {
  const [draft, setDraft] = useState<RegularSettingsDraft>(() => hydrateDraftFromSettings({ roles: roles as never }))
  latestDraft = draft
  return <SignalKRolesSection draft={draft} onChange={(patch) => setDraft((previous) => ({ ...previous, ...patch }))} />
}

function renderRoles(roles?: unknown) {
  return render(
    <SettingsFormProvider>
      <RolesHarness roles={roles} />
    </SettingsFormProvider>,
  )
}

const pendingRoles = () => buildRegularSettingsPatch(latestDraft).roles

describe('Settings -> SignalK: Discovery', () => {
  it('lists what the boat reports by kind, with live readings', async () => {
    render(<SignalKDiscoverySection />)

    await screen.findByText('propulsion.port')
    expect(screen.getByRole('heading', { name: 'Engines' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Batteries' })).toBeInTheDocument()
    // A kind with nothing reporting is not listed, and neither are AC inputs.
    expect(screen.queryByRole('heading', { name: 'Alternators' })).toBeNull()
    expect(screen.queryByText('electrical.ac.5')).toBeNull()
    expect(screen.getByText('RPM 1800 rpm · Coolant 77 °C')).toBeInTheDocument()
    expect(screen.getByText('Voltage 27.20 V · State of charge 79%')).toBeInTheDocument()
  })

  it('shows which item owns a source, and offers Assign and Create for one that no item owns', async () => {
    render(<SignalKDiscoverySection />)

    await screen.findByText('propulsion.port')
    expect(screen.getByText('Port engine')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Assign to item' })).toHaveLength(3)
    expect(screen.getAllByRole('button', { name: 'Create item' })).toHaveLength(3)
  })

  it('says Not reporting for a source an item still owns that the boat has stopped reporting', async () => {
    render(<SignalKDiscoverySection />)

    await screen.findByText('propulsion.wing')
    expect(screen.getByText('Wing engine')).toBeInTheDocument()
    expect(screen.getByText('Not reporting')).toBeInTheDocument()
  })

  it('names what the bus calls a battery, and marks a charger input', async () => {
    render(<SignalKDiscoverySection />)

    await screen.findByText('electrical.batteries.0')
    expect(screen.getByText('The boat calls it Batrium-BMS')).toBeInTheDocument()
    expect(screen.getByText(/Charger input, not a battery/)).toBeInTheDocument()
  })

  it('says so when Helmcentral has no SignalK data, instead of an empty list', async () => {
    discoveryBody = { connected: false, groups: [], detectors: {} }
    render(<SignalKDiscoverySection />)

    expect(await screen.findByText(/no data from SignalK yet/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Create item' })).toBeNull()
  })

  it('assigns a source to an existing item without disturbing its other fields', async () => {
    equipmentItems.find((i) => i.id === 'eng-2')!.notes = 'serviced in March'
    render(<SignalKDiscoverySection />)

    await screen.findByText('propulsion.starboard')
    const starboard = screen.getByText('propulsion.starboard').closest('div.grid') as HTMLElement
    fireEvent.click(within(starboard).getByRole('button', { name: 'Assign to item' }))
    const picker = within(starboard).getByLabelText('Item for propulsion.starboard')
    // Only items that own no source are offered.
    expect(within(picker).queryByRole('option', { name: 'Port engine' })).toBeNull()
    fireEvent.change(picker, { target: { value: 'eng-2' } })
    fireEvent.click(within(starboard).getByRole('button', { name: 'Assign' }))

    await waitFor(() => {
      const put = fetchMock.mock.calls.find(([url, init]) => String(url).endsWith('/api/inventory/equipment/eng-2') && (init as RequestInit | undefined)?.method === 'PUT')
      expect(put).toBeDefined()
      expect(JSON.parse(String((put![1] as RequestInit).body))).toMatchObject({ name: 'Starboard engine', notes: 'serviced in March', signalk_source: 'propulsion.starboard' })
    })
  })

  it('creates an item from just a name and the kind, and maps it', async () => {
    render(<SignalKDiscoverySection />)

    await screen.findByText('propulsion.starboard')
    const starboard = screen.getByText('propulsion.starboard').closest('div.grid') as HTMLElement
    fireEvent.click(within(starboard).getByRole('button', { name: 'Create item' }))
    fireEvent.change(within(starboard).getByLabelText('Name for the new engine item'), { target: { value: 'Starboard main' } })
    fireEvent.click(within(starboard).getByRole('button', { name: 'Create' }))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(([url, init]) => String(url).endsWith('/api/inventory/equipment') && (init as RequestInit | undefined)?.method === 'POST')
      expect(post).toBeDefined()
      expect(JSON.parse(String((post![1] as RequestInit).body))).toMatchObject({ name: 'Starboard main', system: 'propulsion', signalk_source: 'propulsion.starboard' })
    })
  })

  it('shows the server\'s own refusal when a source cannot be assigned', async () => {
    putFailsWith = 'propulsion.starboard is already assigned to "Old engine"; unassign it there first'
    render(<SignalKDiscoverySection />)

    await screen.findByText('propulsion.starboard')
    const starboard = screen.getByText('propulsion.starboard').closest('div.grid') as HTMLElement
    fireEvent.click(within(starboard).getByRole('button', { name: 'Assign to item' }))
    fireEvent.change(within(starboard).getByLabelText('Item for propulsion.starboard'), { target: { value: 'eng-2' } })
    fireEvent.click(within(starboard).getByRole('button', { name: 'Assign' }))

    expect(await within(starboard).findByRole('alert')).toHaveTextContent('already assigned to "Old engine"')
  })
})

describe('Settings -> SignalK: Roles', () => {
  it('offers each role only the items assigned to that kind of source, and Not set', async () => {
    renderRoles()

    const bank = await screen.findByLabelText('House bank')
    await waitFor(() => expect(within(bank).getByRole('option', { name: 'Lithium bank · electrical.batteries.512' })).toBeInTheDocument())
    expect(within(bank).getByRole('option', { name: 'Not set' })).toBeInTheDocument()
    expect(within(bank).getByRole('option', { name: 'Starter battery · electrical.batteries.3' })).toBeInTheDocument()
    expect(within(bank).queryByRole('option', { name: /Port engine/ })).toBeNull()
    expect(within(bank).queryByRole('option', { name: /Spare impeller/ })).toBeNull()
    expect(within(screen.getByLabelText('Generator')).getByRole('option', { name: 'Generator · electrical.generator.0' })).toBeInTheDocument()
    expect((bank as HTMLSelectElement).value).toBe('')
    expect(pendingRoles()).toEqual(emptyVesselRoles)
  })

  it('carries the choices into what a save sends', async () => {
    renderRoles()

    const bank = await screen.findByLabelText('House bank')
    await waitFor(() => expect(within(bank).getByRole('option', { name: 'Lithium bank · electrical.batteries.512' })).toBeInTheDocument())
    fireEvent.change(bank, { target: { value: 'bank-1' } })
    fireEvent.change(screen.getByLabelText('Port alternator'), { target: { value: 'alt-2' } })
    fireEvent.change(screen.getByLabelText('Starboard alternator'), { target: { value: 'alt-12' } })
    fireEvent.change(screen.getByLabelText('Generator AC input'), { target: { value: '5' } })

    await waitFor(() => expect(pendingRoles()).toEqual({
      ...emptyVesselRoles, house_bank: 'bank-1', port_alternator: 'alt-2', starboard_alternator: 'alt-12', generator_ac_instance: '5',
    }))
  })

  it('does not let one alternator be both port and starboard', async () => {
    renderRoles({ ...emptyVesselRoles, port_alternator: 'alt-2' })

    const starboard = await screen.findByLabelText('Starboard alternator')
    await waitFor(() => expect(within(starboard).getByRole('option', { name: 'Port alternator · electrical.alternator.2' })).toBeDisabled())
    expect(within(starboard).getByRole('option', { name: 'Starboard alternator · electrical.alternator.12' })).not.toBeDisabled()
  })

  it('keeps a saved choice whose item has lost its source, so the operator sees what is stored', async () => {
    renderRoles({ ...emptyVesselRoles, shore_charger: 'gone-1' })

    const charger = await screen.findByLabelText('Shore charger')
    expect((charger as HTMLSelectElement).value).toBe('gone-1')
    expect(within(charger).getByRole('option', { name: 'Item no longer assigned' })).toBeInTheDocument()
  })

  it('keeps a saved AC input the boat no longer reports, marked not reporting', async () => {
    renderRoles({ ...emptyVesselRoles, generator_ac_instance: '9' })

    const ac = await screen.findByLabelText('Generator AC input')
    expect(within(ac).getByRole('option', { name: '9 · not reporting' })).toBeInTheDocument()
    expect(within(ac).getByRole('option', { name: '5 · 1250 W' })).toBeInTheDocument()
  })

  it('shows each check as not set up with what is missing', async () => {
    renderRoles()

    expect(await screen.findByText(/Choose your house bank under Roles/)).toBeInTheDocument()
    expect(screen.getByText(/Assign an engine to a propulsion source/)).toBeInTheDocument()
    expect(screen.getByText(/Assign a second engine/)).toBeInTheDocument()
  })

  it('names the server\'s problem when the roles could not be read, and leaves them out of a save', async () => {
    settingsBody = { roles_error: 'roles settings: parsing roles block: bad' }
    render(
      <SettingsFormProvider>
        <UnreadableHarness />
      </SettingsFormProvider>,
    )

    expect(await screen.findByRole('alert')).toHaveTextContent('Roles could not be read: roles settings: parsing roles block: bad')
    expect(buildRegularSettingsPatch(latestDraft)).not.toHaveProperty('roles')
  })
})

function UnreadableHarness() {
  const [draft, setDraft] = useState<RegularSettingsDraft>(() => hydrateDraftFromSettings({}))
  latestDraft = draft
  return <SignalKRolesSection draft={draft} onChange={(patch) => setDraft((previous) => ({ ...previous, ...patch }))} />
}

describe('Discovery with SignalK not connected', () => {
  it('still lists the saved mappings by kind, each marked Not reporting', async () => {
    discoveryBody = { connected: false, groups: [], detectors: {} }
    render(<SignalKDiscoverySection />)
    expect(await screen.findByText(/no data from SignalK yet/)).toBeInTheDocument()
    for (const source of ['propulsion.port', 'electrical.batteries.512', 'electrical.generator.0']) {
      expect(await screen.findByText(source)).toBeInTheDocument()
    }
    expect(screen.getByText('Port engine')).toBeInTheDocument()
    expect(screen.getByText('Lithium bank')).toBeInTheDocument()
    expect(screen.getAllByText('Not reporting').length).toBeGreaterThanOrEqual(6)
    expect(screen.getByRole('heading', { name: 'Engines' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Batteries' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Generators' })).toBeInTheDocument()
  })
})

describe('Assign to item offers only items that could be that kind', () => {
  it('lists the starboard engine for an engine source, not a spare impeller', async () => {
    render(<SignalKDiscoverySection />)
    const starboard = (await screen.findByText('propulsion.starboard')).closest('div')!.parentElement!
    fireEvent.click(within(starboard).getByRole('button', { name: 'Assign to item' }))
    const options = Array.from((await screen.findByLabelText('Item for propulsion.starboard')).querySelectorAll('option')).map((o) => o.textContent)
    expect(options).toEqual(['Choose an item…', 'Starboard engine'])
  })
})
