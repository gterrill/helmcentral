import { describe, it, expect, vi, beforeEach } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useState } from 'react'

import { SignalKDiscoverySection } from '@/components/settings/sections/signalk-discovery-section'
import { SignalKRolesSection } from '@/components/settings/sections/signalk-roles-section'
import { SettingsFormProvider } from '@/components/settings/settings-form-context'
import { buildRegularSettingsPatch, hydrateDraftFromSettings, type RegularSettingsDraft } from '@/components/settings/settings-draft'
import { emptyVesselRoles } from '@/lib/vessel-settings'
import { invalidateQueries } from '@/lib/api-query'

const YOUNG_NOTE = 'Helmcentral has been watching the boat since 18 Oct. A source that is switched off is listed once it has reported since then.'

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
let postFailsWith: string | null
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
      if (postFailsWith) return Promise.resolve({ ok: false, status: 409, json: async () => ({ error: postFailsWith }) })
      const body = JSON.parse(String(init?.body))
      const created = item({ id: 'eq-new', ...body, source_kind: String(body.signalk_source).startsWith('electrical.batteries') ? 'battery' : '' })
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
  postFailsWith = null
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

// The boat as found on 2026-10-21: the engines are off, so propulsion.port and
// starboard are only in Helmcentral's record, and the two Cummins items were
// assigned to propulsion.0 and .1, which only ever carried exhaust temperature.
function engineOffBoat() {
  equipmentItems = [
    item({ id: 'c0', name: 'Cummins QSB 6.7', system: 'propulsion', signalk_source: 'propulsion.0', source_kind: 'engine' }),
    item({ id: 'alt-1', name: 'Port alternator', system: 'electrical', signalk_source: 'electrical.alternator.1', source_kind: 'alternator' }),
  ]
  discoveryBody = {
    ...discoveryBody,
    groups: [
      { kind: 'engine', mappable: true, signature_label: 'RPM', rows: [
        row('propulsion.0', '0', [], { reporting: false, item: { id: 'c0', name: 'Cummins QSB 6.7' }, signature: 'missing', last_seen: null }),
        row('propulsion.port', 'port', [], { reporting: false, signature: 'carried', last_seen: '2026-10-16T12:00:00Z' }),
        row('propulsion.starboard', 'starboard', [], { reporting: false, signature: 'carried', last_seen: '2026-10-16T12:00:00Z' }),
      ] },
      { kind: 'alternator', mappable: true, signature_label: 'output current', rows: [
        row('electrical.alternator.1', '1', [], { reporting: false, item: { id: 'alt-1', name: 'Port alternator' }, signature: 'missing', last_seen: null }),
        row('electrical.alternator.12', '12', [], { reporting: false, signature: 'carried', last_seen: '2026-10-16T12:00:00Z' }),
      ] },
    ],
    tracking: { since: '2026-10-01T00:00:00Z', since_day: '1 Oct', note: '', seeded: true, covers: true, error: '' },
  }
}

describe('Settings -> SignalK: sources that carry their signature', () => {
  it('lists an engine that is off from what the boat has reported and flags the item on a source that never reported RPM', async () => {
    engineOffBoat()
    render(<SignalKDiscoverySection />)

    await screen.findByText('propulsion.port')
    expect(screen.getAllByText('Not reporting now · last seen 16 Oct').length).toBe(3)
    const alert = screen.getByText(/propulsion\.0 reports other readings but has never reported RPM/)
    expect(alert).toHaveAttribute('role', 'alert')
    expect(alert.textContent).toContain('Choose the source that does')
  })

  it('says since when Helmcentral has been watching while the record is young', async () => {
    engineOffBoat()
    discoveryBody = { ...discoveryBody, tracking: { since: '2026-10-18T12:00:00Z', since_day: '18 Oct', note: YOUNG_NOTE, seeded: false, covers: false, error: '' } }
    render(<SignalKDiscoverySection />)

    expect(await screen.findByText(/Helmcentral has been watching the boat since 18 Oct/)).toBeInTheDocument()
    expect(screen.getByText(/switched off is listed once it has reported since then/)).toBeInTheDocument()
  })

  it('says plainly when Helmcentral could not read its record', async () => {
    engineOffBoat()
    discoveryBody = { ...discoveryBody, tracking: { since: null, since_day: '', note: "Helmcentral couldn't read its record of what the boat has reported (database is locked), so only sources reporting now are listed.", seeded: false, covers: false, error: 'database is locked' } }
    render(<SignalKDiscoverySection />)

    expect(await screen.findByText(/couldn't read its record of what the boat has reported \(database is locked\)/)).toBeInTheDocument()
  })

  it('has no watching note once the record covers enough time', async () => {
    engineOffBoat()
    render(<SignalKDiscoverySection />)

    await screen.findByText('propulsion.port')
    expect(screen.queryByText(/Helmcentral has been watching/)).toBeNull()
  })

  it('offers a logged source under From the boat and flags the role whose item is on a source with no signature', async () => {
    engineOffBoat()
    renderRoles({ ...emptyVesselRoles, port_alternator: 'alt-1' })

    const select = await screen.findByLabelText('Port alternator')
    await waitFor(() => expect(within(select).getByRole('option', { name: 'electrical.alternator.12 · not reporting now · last seen 16 Oct' })).toBeInTheDocument())
    expect(await screen.findByText(/electrical\.alternator\.1 reports other readings but has never reported output current/)).toHaveAttribute('role', 'alert')
  })
})

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
    expect(screen.getByText('Not reporting now')).toBeInTheDocument()
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
    // Discovery offers only items with no source, so assigning takes a source
    // from nothing: no dependants are looked up and nothing is confirmed.
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/dependants'))).toBe(false)
  })

  it('has no way to take a source off the item that owns it', async () => {
    render(<SignalKDiscoverySection />)
    await screen.findByText('propulsion.port')
    const port = screen.getByText('propulsion.port').closest('div.grid') as HTMLElement
    expect(within(port).queryByRole('button', { name: /assign|unassign|clear|remove/i })).toBeNull()
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

describe('Settings -> SignalK -> Roles: choosing from what the boat reports', () => {
  it('lists unassigned reporting sources under "From the boat" with a short live reading', async () => {
    renderRoles()

    const bank = await screen.findByLabelText('House bank')
    const group = await waitFor(() => {
      const found = bank.querySelector('optgroup[label="From the boat"]')
      expect(found).not.toBeNull()
      return found as HTMLElement
    })
    // The charger input is not a battery; only the one battery nobody owns is offered.
    expect(within(group).getAllByRole('option').map((o) => o.textContent)).toEqual(['Batrium-BMS · electrical.batteries.0 · 27.20 V · 79%'])
    // Items already assigned to the kind come first, before the group.
    const labels = Array.from(bank.querySelectorAll('option')).map((o) => o.textContent)
    expect(labels.indexOf('Lithium bank · electrical.batteries.512')).toBeLessThan(labels.indexOf('Batrium-BMS · electrical.batteries.0 · 27.20 V · 79%'))
  })

  it('creates an item named after the role, then sets the role to it', async () => {
    renderRoles()

    const bank = await screen.findByLabelText('House bank')
    await waitFor(() => expect(bank.querySelector('optgroup[label="From the boat"]')).not.toBeNull())
    fireEvent.change(bank, { target: { value: 'source:electrical.batteries.0' } })

    await waitFor(() => expect(pendingRoles()).toEqual({ ...emptyVesselRoles, house_bank: 'eq-new' }))
    const post = fetchMock.mock.calls.find(([url, init]) => String(url).endsWith('/api/inventory/equipment') && (init as RequestInit | undefined)?.method === 'POST')
    expect(JSON.parse(String((post![1] as RequestInit).body))).toMatchObject({ name: 'House bank', system: 'electrical', signalk_source: 'electrical.batteries.0' })
  })

  it('never flashes "Item no longer assigned" while the new item is still being read back', async () => {
    renderRoles()
    const bank = await screen.findByLabelText('House bank')
    await waitFor(() => expect(bank.querySelector('optgroup[label="From the boat"]')).not.toBeNull())
    fireEvent.change(bank, { target: { value: 'source:electrical.batteries.0' } })

    await waitFor(() => expect((bank as HTMLSelectElement).value).toBe('eq-new'))
    expect(within(bank).queryByRole('option', { name: 'Item no longer assigned' })).toBeNull()
    expect(within(bank).getByRole('option', { name: 'House bank · electrical.batteries.0' })).toBeInTheDocument()
  })

  it('says in the help text that the item is made at once and the role waits for Save', async () => {
    renderRoles()
    expect(await screen.findByText(/creates its item straight away; the role itself is saved with Save/)).toBeInTheDocument()
  })

  it('shows the error and leaves the role unchanged when the item cannot be created', async () => {
    postFailsWith = 'electrical.batteries.0 is already assigned to "Old bank"'
    renderRoles()

    const bank = await screen.findByLabelText('House bank')
    await waitFor(() => expect(bank.querySelector('optgroup[label="From the boat"]')).not.toBeNull())
    fireEvent.change(bank, { target: { value: 'source:electrical.batteries.0' } })

    expect(await screen.findByRole('alert')).toHaveTextContent('already assigned to "Old bank"')
    expect(pendingRoles()).toEqual(emptyVesselRoles)
    expect((bank as HTMLSelectElement).value).toBe('')
  })

  it('leaves the boat\'s own name out when it gives none', async () => {
    renderRoles()
    const generator = await screen.findByLabelText('Generator AC input')
    expect(within(generator).getByRole('option', { name: '5 · 1250 W' })).toBeInTheDocument()
    const alt = await screen.findByLabelText('Port alternator')
    expect(alt.querySelector('optgroup')).toBeNull()
  })

  it('disables the generator AC input, with a hint, when the boat has no generator', async () => {
    renderRoles({ ...emptyVesselRoles, generator: 'none' })
    const ac = await screen.findByLabelText('Generator AC input')
    expect(ac).toBeDisabled()
    expect(screen.getByText(/no generator/i)).toBeInTheDocument()
  })

  it('does not list a saved "none" AC input as an instance that is not reporting', async () => {
    renderRoles({ ...emptyVesselRoles, generator_ac_instance: 'none' })

    const ac = await screen.findByLabelText('Generator AC input')
    expect((ac as HTMLSelectElement).value).toBe('none')
    expect(within(ac).getAllByRole('option', { name: 'This boat has none' })).toHaveLength(1)
    expect(within(ac).queryByRole('option', { name: /not reporting/ })).toBeNull()
  })

  it('clears the generator AC input when the generator is set to none', async () => {
    renderRoles({ ...emptyVesselRoles, generator: 'gen-1', generator_ac_instance: '5' })

    const generator = await screen.findByLabelText('Generator')
    fireEvent.change(generator, { target: { value: 'none' } })

    await waitFor(() => expect(pendingRoles()).toEqual({ ...emptyVesselRoles, generator: 'none', generator_ac_instance: '' }))
  })

  it('says the boat is not reporting any of a kind when there is nothing to choose', async () => {
    renderRoles()

    expect(await screen.findByText("The boat isn't reporting any chargers.")).toBeInTheDocument()
    expect(screen.getByText("The boat isn't reporting any inverters.")).toBeInTheDocument()
    // A kind with items to choose from needs no such line.
    expect(screen.queryByText("The boat isn't reporting any batteries.")).toBeNull()
  })

  it('says SignalK is not connected instead, when it is not', async () => {
    discoveryBody = { connected: false, groups: [], detectors: {} }
    equipmentItems = []
    renderRoles()

    expect(await screen.findAllByText("SignalK isn't connected.")).not.toHaveLength(0)
    expect(screen.queryByText(/isn't reporting any/)).toBeNull()
  })
})

describe('Settings -> SignalK -> Roles: This boat has none', () => {
  it('offers it for the parts a boat can lack, and not for the house bank or the voltage readout battery', async () => {
    renderRoles()

    await screen.findByLabelText('House bank')
    for (const label of ['Port alternator', 'Starboard alternator', 'Shore charger', 'Inverter', 'Generator', 'Generator AC input']) {
      expect(within(screen.getByLabelText(label)).getByRole('option', { name: 'This boat has none' })).toBeInTheDocument()
    }
    expect(within(screen.getByLabelText('House bank')).queryByRole('option', { name: 'This boat has none' })).toBeNull()
    expect(within(screen.getByLabelText('Voltage readout battery')).queryByRole('option', { name: 'This boat has none' })).toBeNull()
  })

  it('stores it as "none", distinct from Not set, and shows it as chosen', async () => {
    renderRoles()

    const inverter = await screen.findByLabelText('Inverter')
    fireEvent.change(inverter, { target: { value: 'none' } })

    await waitFor(() => expect(pendingRoles()).toEqual({ ...emptyVesselRoles, inverter: 'none' }))
    expect((inverter as HTMLSelectElement).value).toBe('none')
    expect(within(inverter).getByRole('option', { name: 'Not set' })).toBeInTheDocument()
  })

  it('does not treat "none" on both alternators as a clash', async () => {
    renderRoles({ ...emptyVesselRoles, port_alternator: 'none' })

    const starboard = await screen.findByLabelText('Starboard alternator')
    expect(within(starboard).getByRole('option', { name: 'This boat has none' })).not.toBeDisabled()
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
    expect(screen.getAllByText('Not reporting now').length).toBeGreaterThanOrEqual(6)
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
