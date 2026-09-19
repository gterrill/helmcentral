/**
 * Notification delivery used to live inside the Alarms drawer, wedged
 * between the rule list and the history log. It is configuration, not
 * operational state, so it belongs on the Settings page under its own
 * "Alarms" section — the drawer is what you look at when something is
 * going off, and it should stay that.
 *
 * Moving it there means it must behave like a settings section rather
 * than a self-contained panel: no Save button of its own (two adjacent
 * buttons that look alike is the UX this replaces), edits feed the
 * page's single dirty signal, and the page's one "Save Settings" —
 * plus App.tsx's "Save and Continue" handle — persists the transport
 * config alongside everything else.
 */
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { createRef } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AlarmsDrawer } from '@/components/alarms-drawer'
import { SettingsPage, type SettingsPageHandle } from '@/components/settings/settings-page'
import type { AlarmTransportConfig } from '@/hooks/use-alarm-transports'
import type { SecretKey } from '@/hooks/use-secrets-status'

const saveSettingsMock = vi.fn().mockResolvedValue({})
const saveTransportsMock = vi.fn().mockResolvedValue(undefined)

vi.mock('@/hooks/use-settings-form', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-settings-form')>()),
  useSettingsForm: () => ({
    settings: {}, setSettings: vi.fn(), loading: false, saving: false, error: null,
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
      setFieldValue: vi.fn(), saveTouchedKeys: vi.fn().mockResolvedValue({}), clearKey: vi.fn(),
    }),
  }
})

// `config` is built once inside the factory, so it keeps a stable identity
// across renders — handing back a fresh object each call would retrigger the
// provider's re-seed effect forever. `loadError` goes through vi.hoisted so a
// test can mutate it (the factory is hoisted above plain top-level `let`s).
const transportsState = vi.hoisted(() => ({ loaded: true, loadError: null as string | null }))

vi.mock('@/hooks/use-alarm-transports', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/use-alarm-transports')>()
  const config = actual.emptyTransportConfig()
  return {
    ...actual,
    useAlarmTransports: () => ({
      config, secretsPresent: {}, loading: false,
      loaded: transportsState.loaded, error: transportsState.loadError,
      save: saveTransportsMock, test: vi.fn().mockResolvedValue(undefined),
      testResults: null, testing: false,
    }),
  }
})

vi.mock('@/hooks/use-web-push', () => ({
  useWebPush: () => ({
    support: { kind: 'ok' }, permission: 'default', subscribed: false, deviceCount: 0,
    busy: false, error: null,
    enableOnThisDevice: vi.fn().mockResolvedValue(undefined),
    disableOnThisDevice: vi.fn().mockResolvedValue(undefined),
  }),
}))

vi.mock('@/hooks/use-alarm-rules', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarm-rules')>()),
  useAlarmRules: () => ({
    rules: [], loading: false, error: null,
    createRule: vi.fn(), updateRule: vi.fn(), deleteRule: vi.fn(),
  }),
  useAlarmLog: () => ({ entries: [], refresh: vi.fn().mockResolvedValue(undefined) }),
}))

beforeEach(() => {
  saveSettingsMock.mockReset().mockResolvedValue({})
  saveTransportsMock.mockReset().mockResolvedValue(undefined)
  transportsState.loaded = true
  transportsState.loadError = null
})

function renderSettings(props: Partial<{ onDirtyChange: (d: boolean) => void }> = {}) {
  return render(
    <SettingsPage {...props} />,
  )
}

const openAlarms = () => fireEvent.click(screen.getByRole('button', { name: 'Alarms' }))

describe('Settings Alarms section', () => {
  it('shows the notifications panel once the Alarms section is selected', () => {
    renderSettings()

    // General is the landing section, so nothing alarm-related yet.
    expect(screen.queryByRole('button', { name: /send test/i })).not.toBeInTheDocument()

    openAlarms()

    expect(screen.getByRole('button', { name: /send test/i })).toBeInTheDocument()
    expect(screen.getByLabelText(/ntfy \(phone push\)/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/publish to signalk/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/web push/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/alarm if no signalk data for/i)).toBeInTheDocument()
  })

  it('offers exactly one save control — the page\'s own', () => {
    renderSettings()
    openAlarms()

    expect(screen.queryByRole('button', { name: /save notifications/i })).not.toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /^save/i })).toHaveLength(1)
    expect(screen.getByRole('button', { name: /save settings/i })).toBeInTheDocument()
  })
})

describe('Settings Alarms section saving', () => {
  it('marks the page dirty when a transport is toggled, and clean again after saving', async () => {
    const onDirtyChange = vi.fn()
    renderSettings({ onDirtyChange })

    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
    onDirtyChange.mockClear()

    openAlarms()
    fireEvent.click(screen.getByLabelText(/webhook/i))

    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))

    fireEvent.click(screen.getByRole('button', { name: /save settings/i }))

    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
  })

  it('persists the transport config through the page\'s Save Settings button', async () => {
    renderSettings()
    openAlarms()

    fireEvent.click(screen.getByLabelText(/webhook/i))
    fireEvent.change(screen.getByLabelText(/^url$/i), { target: { value: 'https://hass.local/hook' } })
    fireEvent.click(screen.getByRole('button', { name: /save settings/i }))

    await waitFor(() => expect(saveTransportsMock).toHaveBeenCalledTimes(1))
    const [config] = saveTransportsMock.mock.calls[0] as [AlarmTransportConfig]
    expect(config.webhook).toEqual({ enabled: true, url: 'https://hass.local/hook' })
    // The regular settings patch still goes out on the same click.
    expect(saveSettingsMock).toHaveBeenCalledTimes(1)
  })

  it('carries transport secrets, then clears the entered value once saved', async () => {
    renderSettings()
    openAlarms()

    fireEvent.click(screen.getByLabelText(/ntfy \(phone push\)/i))
    const token = screen.getByLabelText(/access token/i) as HTMLInputElement
    fireEvent.change(token, { target: { value: 'tk_live' } })
    fireEvent.click(screen.getByRole('button', { name: /save settings/i }))

    await waitFor(() => expect(saveTransportsMock).toHaveBeenCalledTimes(1))
    expect(saveTransportsMock.mock.calls[0][1]).toEqual({ NTFY_TOKEN: 'tk_live' })
    await waitFor(() => expect(token.value).toBe(''))
  })

  it('persists transports through the imperative handle App.tsx uses for "Save and Continue"', async () => {
    const ref = createRef<SettingsPageHandle>()
    render(<SettingsPage ref={ref} />)

    openAlarms()
    fireEvent.click(screen.getByLabelText(/webhook/i))

    await ref.current!.save()

    expect(saveTransportsMock).toHaveBeenCalledTimes(1)
    expect(saveSettingsMock).toHaveBeenCalledTimes(1)
  })

  /**
   * The panel's own Save only ever fired when someone was looking at it.
   * The page's Save fires from every section, so an untouched panel must
   * not POST — and after a failed load, "untouched" is an empty config that
   * would otherwise overwrite a good one on the server.
   */
  it('does not write the transport config when the panel was not touched', async () => {
    renderSettings()
    openAlarms()

    fireEvent.click(screen.getByRole('button', { name: /save settings/i }))

    await waitFor(() => expect(saveSettingsMock).toHaveBeenCalledTimes(1))
    expect(saveTransportsMock).not.toHaveBeenCalled()
  })

  it('refuses to save over a transport config that never loaded, and says so', async () => {
    transportsState.loaded = false
    transportsState.loadError = 'alarm transports: HTTP 500'
    renderSettings()
    openAlarms()

    fireEvent.click(screen.getByLabelText(/webhook/i))
    fireEvent.click(screen.getByRole('button', { name: /save settings/i }))

    expect(await screen.findByText(/notifications did not load/i)).toBeInTheDocument()
    expect(saveTransportsMock).not.toHaveBeenCalled()
  })

  it('surfaces a transport save failure instead of reporting success', async () => {
    saveTransportsMock.mockRejectedValueOnce(new Error('alarm-transports unreachable'))
    renderSettings()
    openAlarms()

    fireEvent.click(screen.getByLabelText(/webhook/i))
    fireEvent.click(screen.getByRole('button', { name: /save settings/i }))

    expect(await screen.findByText(/alarm-transports unreachable/i)).toBeInTheDocument()
    expect(screen.queryByText(/^settings saved$/i)).not.toBeInTheDocument()
  })
})

describe('AlarmsDrawer', () => {
  it('no longer carries the notifications panel', () => {
    render(
      <AlarmsDrawer
        alarms={[]}
        onAcknowledge={vi.fn()}
        onSilence={vi.fn()}
        rules={[]}
        loading={false}
        error={null}
        createRule={vi.fn()}
        updateRule={vi.fn()}
        deleteRule={vi.fn()}
        collisionTuningUrl={null}
        forecastWarnings={null}
      />,
    )

    expect(screen.getByText('Active Alarms')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /send test/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /save notifications/i })).not.toBeInTheDocument()
  })
})
