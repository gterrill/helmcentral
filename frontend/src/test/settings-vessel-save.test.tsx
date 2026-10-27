/**
 * The Vessel section used to carry two Save buttons of its own, one for
 * engines and the house bank (a separate endpoint) and one for the
 * particulars, and edits there were invisible to the navigation guard. Both
 * now ride the page's Save bar: engines and the house bank inside the
 * settings request, the particulars as their own request beside it.
 */
import { fireEvent, render as rtlRender, screen, waitFor, within } from '@testing-library/react'
import type { ReactElement } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'
import { settingsFormMock } from '@/test/helpers/settings-form-mock'

import { emptyVesselRoles } from '@/lib/vessel-settings'
import { SettingsPage } from '@/components/settings/settings-page'
import type { SecretKey } from '@/hooks/use-secrets-status'

function render(ui: ReactElement) {
  return rtlRender(
    <div>
      <header data-testid="header" className="relative"><div id="save-bar-slot" /></header>
      {ui}
    </div>,
  )
}
const saveBar = () => within(screen.getByTestId('header'))

const saveSettingsMock = vi.fn()

vi.mock('@/hooks/use-settings-form', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-settings-form')>()),
  useSettingsForm: () => settingsFormMock(saveSettingsMock, settingsFixture),
}))

vi.mock('@/hooks/use-secrets-status', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/use-secrets-status')>()
  return {
    ...actual,
    useSecretsStatus: () => ({
      status: Object.fromEntries(actual.SECRET_KEYS.map((key: SecretKey) => [key, false])),
      values: Object.fromEntries(actual.SECRET_KEYS.map((key: SecretKey) => [key, ''])),
      touched: Object.fromEntries(actual.SECRET_KEYS.map((key: SecretKey) => [key, false])),
      loading: false, error: null,
      setFieldValue: vi.fn(), saveTouchedKeys: vi.fn().mockResolvedValue({}), clearKey: vi.fn(), resetTouched: vi.fn(),
    }),
  }
})

vi.mock('@/hooks/use-alarm-transports', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/use-alarm-transports')>()
  const config = actual.emptyTransportConfig()
  return {
    ...actual,
    useAlarmTransports: () => ({
      config, secretsPresent: {}, loading: false, loaded: true, error: null,
      save: vi.fn().mockResolvedValue(undefined), test: vi.fn().mockResolvedValue(undefined),
      testResults: null, testing: false,
    }),
  }
})

const stream = installFakeTelemetryStream()
beforeEach(() => { stream.seed('vessel-state', { name: 'Pikorua', vessel_prefix: 'M/V', length_overall_m: 17.9 }) })

// Stable identity per test: the mocked hook hands this back on every render.
const defaultSettings = () => ({ roles: { ...emptyVesselRoles } }) as Record<string, unknown>
let settingsFixture = defaultSettings()

const particularsStored = {
  builder: 'Granocean', model: 'W-60', year: 2024, hin: '', flag: 'Cook Islands', hailing_port: '',
  hull_type: '', hull_material: '', displacement_kg: null, shore_power: '', system_voltage: '',
  registration: '', imo: '', epirb_id: '', date_acquired: '', loa_m: null, beam_m: null, owner_name: '', owner_phone: '', owner_email: '', insurer: '', policy_number: '', home_marina: '', berth: '', storm_delegate: '', updated_at: '2026-10-01T05:00:00Z',
}

const bankItem = {
  id: 'bank-1', name: 'Lithium bank', category: 'general', system: 'electrical', manufacturer: '', model: '', serial: '', quantity: 1, part_number: '',
  required_quantity: null, status: 'deployed', zone_id: null, bin_id: null, zone_name: '', bin_code: '', location_detail: '', install_date: '',
  signalk_source: 'electrical.batteries.512', capacity_ah: 0, cells: 0, warn_voltage: 0, high_voltage: 0, tank_warn_percent: 0, tank_alarm_percent: 0, tank_alarms_off: false, source_kind: 'battery', hour_meter_source: '',
  profile_id: '', aliases: [], verified_aboard: false, notes: '', link_count: 0, created_at: '', updated_at: '', photo_ids: [], exclusive_photo_ids: [],
}

let particularsPut: (body: Record<string, unknown>) => { ok: boolean; status?: number; body: unknown }
const fetchMock = vi.fn()
const putCalls = () => fetchMock.mock.calls.filter(([, init]) => (init as RequestInit | undefined)?.method === 'PUT')

beforeEach(() => {
  settingsFixture = defaultSettings()
  saveSettingsMock.mockReset().mockResolvedValue({})
  particularsPut = (body) => ({ ok: true, body: { ...body, updated_at: '2026-10-01T06:00:00Z' } })
  fetchMock.mockReset().mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    if (u.endsWith('/api/vessel/discovery')) {
      return Promise.resolve({ ok: true, json: async () => ({ connected: true, groups: [], detectors: {} }) })
    }
    if (u.includes('/api/inventory/equipment')) {
      return Promise.resolve({ ok: true, json: async () => ({ items: [bankItem] }) })
    }
    if (u.endsWith('/api/vessel/particulars') && init?.method === 'PUT') {
      const result = particularsPut(JSON.parse(String(init.body)))
      return Promise.resolve({ ok: result.ok, status: result.status ?? 200, json: async () => result.body })
    }
    if (u.endsWith('/api/vessel/particulars')) {
      return Promise.resolve({ ok: true, json: async () => particularsStored })
    }
    if (u.endsWith('/api/alarms/tank-levels')) {
      return Promise.resolve({ ok: true, json: async () => ({ fuel: { warn: 20, alarm: 10 }, water: { warn: 20, alarm: 10 }, waste: { warn: 80, alarm: 90 } }) })
    }
    return Promise.resolve({ ok: true, json: async () => ({}) })
  })
  vi.stubGlobal('fetch', fetchMock)
})

async function openVessel(onDirtyChange?: (dirty: boolean) => void) {
  render(<SettingsPage onOpenHelp={vi.fn()} onDirtyChange={onDirtyChange} />)
  fireEvent.click(screen.getByRole('button', { name: 'Vessel' }))
  await screen.findByLabelText('Builder')
}

async function chooseHouseBank() {
  fireEvent.click(screen.getByRole('button', { name: 'SignalK' }))
  const bank = await screen.findByLabelText('House bank')
  await waitFor(() => expect(within(bank).getByRole('option', { name: 'Lithium bank · electrical.batteries.512' })).toBeInTheDocument())
  fireEvent.change(bank, { target: { value: 'bank-1' } })
  fireEvent.click(screen.getByRole('button', { name: 'Vessel' }))
  await screen.findByLabelText('Builder')
}

describe('Settings -> Vessel and the Save bar', () => {
  it('has no Save Vessel Settings or Save particulars button, and no engines or power blocks', async () => {
    await openVessel()
    expect(screen.queryByRole('button', { name: /save vessel settings/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /save particulars/i })).toBeNull()
    expect(screen.queryByRole('heading', { name: 'Engines' })).toBeNull()
  })

  it('choosing a role shows the bar and marks the page dirty; choosing Not set again clears both', async () => {
    const onDirtyChange = vi.fn()
    await openVessel(onDirtyChange)
    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))

    await chooseHouseBank()
    expect(await saveBar().findByText('Unsaved changes')).toBeInTheDocument()
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)

    fireEvent.click(screen.getByRole('button', { name: 'SignalK' }))
    fireEvent.change(await screen.findByLabelText('House bank'), { target: { value: '' } })
    await waitFor(() => expect(saveBar().queryByText('Unsaved changes')).not.toBeInTheDocument())
    expect(onDirtyChange).toHaveBeenLastCalledWith(false)
  })

  it('editing a particulars field shows the bar and marks the page dirty', async () => {
    const onDirtyChange = vi.fn()
    await openVessel(onDirtyChange)

    fireEvent.change(screen.getByLabelText('Flag'), { target: { value: 'Australia' } })

    expect(await saveBar().findByText('Unsaved changes')).toBeInTheDocument()
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
  })

  it('Save sends the roles in the settings request and the particulars to their own endpoint', async () => {
    await openVessel()
    await chooseHouseBank()
    fireEvent.change(screen.getByLabelText('Flag'), { target: { value: 'Australia' } })

    fireEvent.click(saveBar().getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(saveSettingsMock).toHaveBeenCalledTimes(1))
    const patch = saveSettingsMock.mock.calls[0][0] as { roles?: { house_bank: string } }
    expect(patch.roles?.house_bank).toBe('bank-1')

    await waitFor(() => expect(putCalls()).toHaveLength(1))
    const [url, init] = putCalls()[0]
    expect(url).toBe('/api/vessel/particulars')
    expect(JSON.parse(String((init as RequestInit).body))).toMatchObject({ flag: 'Australia', builder: 'Granocean' })
    expect(fetchMock.mock.calls.some(([u]) => String(u).endsWith('/api/vessel'))).toBe(false)

    await waitFor(() => expect(saveBar().queryByText('Unsaved changes')).not.toBeInTheDocument())
  })

  it('a roles-only edit does not send particulars when nothing changed there', async () => {
    await openVessel()
    await chooseHouseBank()

    fireEvent.click(saveBar().getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(saveSettingsMock).toHaveBeenCalledTimes(1))
    expect(putCalls()).toHaveLength(0)
  })

  it('Discard restores the roles and the particulars', async () => {
    await openVessel()
    await chooseHouseBank()
    const flag = screen.getByLabelText('Flag') as HTMLInputElement
    fireEvent.change(flag, { target: { value: 'Australia' } })

    fireEvent.click(await saveBar().findByRole('button', { name: 'Discard' }))

    await waitFor(() => expect(flag.value).toBe('Cook Islands'))
    expect(saveBar().queryByText('Unsaved changes')).not.toBeInTheDocument()
    expect(saveSettingsMock).not.toHaveBeenCalled()
    expect(putCalls()).toHaveLength(0)
  })

  it('a particulars refusal shows in the bar and at the field, while the settings part still saved', async () => {
    particularsPut = () => ({ ok: false, status: 400, body: { field: 'year', message: 'year must be between 1800 and 2200' } })
    await openVessel()
    await chooseHouseBank()
    fireEvent.change(screen.getByLabelText('Year built'), { target: { value: '3000' } })

    fireEvent.click(saveBar().getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(saveBar().getByRole('alert')).toHaveTextContent('year must be between 1800 and 2200'))
    expect(screen.getAllByText('year must be between 1800 and 2200').length).toBeGreaterThanOrEqual(2)
    expect(saveSettingsMock).toHaveBeenCalledTimes(1)

    fireEvent.click(saveBar().getByRole('button', { name: 'Discard' }))
    await waitFor(() => expect((screen.getByLabelText('Year built') as HTMLInputElement).value).toBe('2024'))
  })
})

describe('roles the server could not read', () => {
  it('shows the server message, and a save from another field leaves roles out', async () => {
    settingsFixture = { roles_error: 'roles settings: parsing roles block: bad' }
    render(<SettingsPage onOpenHelp={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: 'SignalK' }))

    expect(await screen.findByText(/Roles could not be read/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Vessel' }))
    fireEvent.change(screen.getByLabelText('Vessel prefix'), { target: { value: 'S/V Test' } })
    fireEvent.click(await saveBar().findByRole('button', { name: 'Save' }))

    await waitFor(() => expect(saveSettingsMock).toHaveBeenCalledTimes(1))
    const patch = saveSettingsMock.mock.calls[0][0] as Record<string, unknown>
    expect(patch).not.toHaveProperty('roles')
  })
})

describe('the roles part of the settings draft', () => {
  it('is left out of the patch when the server block was never read, so a save cannot wipe it', async () => {
    const { buildRegularSettingsPatch, hydrateDraftFromSettings } = await import('@/components/settings/settings-draft')
    expect(buildRegularSettingsPatch(hydrateDraftFromSettings({})).roles).toBeUndefined()
    expect(buildRegularSettingsPatch(hydrateDraftFromSettings({ roles: { ...emptyVesselRoles } })).roles).toEqual(emptyVesselRoles)
  })
})
