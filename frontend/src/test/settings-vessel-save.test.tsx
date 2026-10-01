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
  useSettingsForm: () => ({
    settings: settingsFixture, setSettings: vi.fn(), loading: false, saving: false, error: null,
    save: saveSettingsMock,
  }),
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

vi.mock('@/hooks/use-vessel-identity', () => ({ useVesselIdentity: () => ({ boatName: 'M/V Pikorua' }) }))
vi.mock('@/hooks/use-vessel-state', () => ({ useVesselState: () => ({ vesselLengthOverallM: 17.9, vesselDraftM: null }) }))

// Stable identity per test: the mocked hook hands this back on every render.
const defaultSettings = () => ({
  vessel: { engines: [{ instance: 'port', name: 'Port', equipment_id: '' }], house_bank: null },
}) as Record<string, unknown>
let settingsFixture = defaultSettings()

const particularsStored = {
  builder: 'Granocean', model: 'W-60', year: 2024, hin: '', flag: 'Cook Islands', hailing_port: '',
  hull_type: '', hull_material: '', displacement_kg: null, shore_power: '', system_voltage: '',
  registration: '', imo: '', epirb_id: '', date_acquired: '', updated_at: '2026-10-01T05:00:00Z',
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
    if (u.endsWith('/api/vessel/candidates')) {
      return Promise.resolve({
        ok: true,
        json: async () => ({
          engines: [{ instance: 'port', rpm: 1800, coolant_c: 76 }, { instance: 'starboard', rpm: 1810, coolant_c: 75 }],
          batteries: [], detectors: {},
        }),
      })
    }
    if (u.endsWith('/api/vessel/particulars') && init?.method === 'PUT') {
      const result = particularsPut(JSON.parse(String(init.body)))
      return Promise.resolve({ ok: result.ok, status: result.status ?? 200, json: async () => result.body })
    }
    if (u.endsWith('/api/vessel/particulars')) {
      return Promise.resolve({ ok: true, json: async () => particularsStored })
    }
    return Promise.resolve({ ok: true, json: async () => ({}) })
  })
  vi.stubGlobal('fetch', fetchMock)
})

async function openVessel(onDirtyChange?: (dirty: boolean) => void) {
  render(<SettingsPage onDirtyChange={onDirtyChange} />)
  fireEvent.click(screen.getByRole('button', { name: 'Vessel' }))
  await screen.findByLabelText('Builder')
  await screen.findByText('propulsion.starboard')
}

describe('Settings -> Vessel and the Save bar', () => {
  it('has no Save Vessel Settings or Save particulars button', async () => {
    await openVessel()
    expect(screen.queryByRole('button', { name: /save vessel settings/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /save particulars/i })).toBeNull()
  })

  it('ticking an engine shows the bar and marks the page dirty; unticking it again clears both', async () => {
    const onDirtyChange = vi.fn()
    await openVessel(onDirtyChange)
    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))

    fireEvent.click(screen.getByLabelText('Include starboard in anomaly detection'))
    expect(await saveBar().findByText('Unsaved changes')).toBeInTheDocument()
    expect(onDirtyChange).toHaveBeenLastCalledWith(true)

    fireEvent.click(screen.getByLabelText('Include starboard in anomaly detection'))
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

  it('Save sends the vessel block in the settings request and the particulars to their own endpoint', async () => {
    await openVessel()
    fireEvent.click(screen.getByLabelText('Include starboard in anomaly detection'))
    fireEvent.change(screen.getByLabelText('Flag'), { target: { value: 'Australia' } })

    fireEvent.click(saveBar().getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(saveSettingsMock).toHaveBeenCalledTimes(1))
    const patch = saveSettingsMock.mock.calls[0][0] as { vessel?: { engines: { instance: string }[]; house_bank: unknown } }
    expect(patch.vessel?.engines.map((e) => e.instance)).toEqual(['port', 'starboard'])
    expect(patch.vessel?.house_bank).toBeNull()

    await waitFor(() => expect(putCalls()).toHaveLength(1))
    const [url, init] = putCalls()[0]
    expect(url).toBe('/api/vessel/particulars')
    expect(JSON.parse(String((init as RequestInit).body))).toMatchObject({ flag: 'Australia', builder: 'Granocean' })
    // Nothing is POSTed to the retired vessel endpoint.
    expect(fetchMock.mock.calls.some(([u]) => String(u).endsWith('/api/vessel'))).toBe(false)

    await waitFor(() => expect(saveBar().queryByText('Unsaved changes')).not.toBeInTheDocument())
  })

  it('a particulars-only edit does not send particulars when nothing changed there', async () => {
    await openVessel()
    fireEvent.click(screen.getByLabelText('Include starboard in anomaly detection'))

    fireEvent.click(saveBar().getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(saveSettingsMock).toHaveBeenCalledTimes(1))
    expect(putCalls()).toHaveLength(0)
  })

  it('Discard restores the engines and the particulars', async () => {
    await openVessel()
    const starboard = screen.getByLabelText('Include starboard in anomaly detection')
    const flag = screen.getByLabelText('Flag') as HTMLInputElement
    fireEvent.click(starboard)
    fireEvent.change(flag, { target: { value: 'Australia' } })

    fireEvent.click(await saveBar().findByRole('button', { name: 'Discard' }))

    await waitFor(() => expect(flag.value).toBe('Cook Islands'))
    expect(screen.getByLabelText('Include starboard in anomaly detection')).not.toBeChecked()
    expect(saveBar().queryByText('Unsaved changes')).not.toBeInTheDocument()
    expect(saveSettingsMock).not.toHaveBeenCalled()
    expect(putCalls()).toHaveLength(0)
  })

  it('a particulars refusal shows in the bar and at the field, while the settings part still saved', async () => {
    particularsPut = () => ({ ok: false, status: 400, body: { field: 'year', message: 'year must be between 1800 and 2200' } })
    await openVessel()
    fireEvent.click(screen.getByLabelText('Include starboard in anomaly detection'))
    fireEvent.change(screen.getByLabelText('Year built'), { target: { value: '3000' } })

    fireEvent.click(saveBar().getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(saveBar().getByRole('alert')).toHaveTextContent('year must be between 1800 and 2200'))
    expect(screen.getAllByText('year must be between 1800 and 2200').length).toBeGreaterThanOrEqual(2)
    expect(saveSettingsMock).toHaveBeenCalledTimes(1)

    // The settings snapshot moved with its own request, so Discard rolls back
    // only what is still unsaved: the engine stays ticked, the year goes back.
    fireEvent.click(saveBar().getByRole('button', { name: 'Discard' }))
    await waitFor(() => expect((screen.getByLabelText('Year built') as HTMLInputElement).value).toBe('2024'))
    expect(screen.getByLabelText('Include starboard in anomaly detection')).toBeChecked()
  })
})

describe('a vessel block the server could not read', () => {
  it('shows the server message, and a save from another field leaves vessel out', async () => {
    settingsFixture = { vessel_error: 'vessel settings: parsing vessel block: bad engines' }
    render(<SettingsPage />)
    fireEvent.click(screen.getByRole('button', { name: 'Vessel' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('bad engines')
    expect(screen.queryByLabelText(/include .* in anomaly detection/i)).toBeNull()

    fireEvent.change(screen.getByLabelText('Vessel prefix'), { target: { value: 'S/V Test' } })
    fireEvent.click(await saveBar().findByRole('button', { name: 'Save' }))

    await waitFor(() => expect(saveSettingsMock).toHaveBeenCalledTimes(1))
    const patch = saveSettingsMock.mock.calls[0][0] as Record<string, unknown>
    expect(patch).not.toHaveProperty('vessel')
  })
})

describe('a normal load', () => {
  it('never raises an alert in the Vessel section, even on the first render after settings resolve', async () => {
    render(<SettingsPage />)
    fireEvent.click(screen.getByRole('button', { name: 'Vessel' }))
    expect(screen.queryByRole('alert')).toBeNull()
    await screen.findByText('propulsion.starboard')
    expect(screen.queryByRole('alert')).toBeNull()
  })
})

describe('the vessel part of the settings draft', () => {
  it('is left out of the patch when the server block was never read, so a save cannot wipe it', async () => {
    const { buildRegularSettingsPatch, hydrateDraftFromSettings } = await import('@/components/settings/settings-draft')
    expect(buildRegularSettingsPatch(hydrateDraftFromSettings({})).vessel).toBeUndefined()
    expect(buildRegularSettingsPatch(hydrateDraftFromSettings({ vessel: { engines: [], house_bank: null } })).vessel)
      .toEqual({ engines: [], house_bank: null })
  })
})
