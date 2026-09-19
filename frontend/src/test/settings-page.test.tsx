/**
 * Covers the Settings page's "dirty" tracking (used by App.tsx to show the
 * sidebar amber-dot indicator and to gate the navigation-guard dialog) and
 * its imperative `save()` handle (used by App.tsx's "Save and Continue"
 * action). Both `@/hooks/use-settings-form` and `@/hooks/use-secrets-status`
 * are mocked here (real exports like `SECRET_KEYS` preserved via
 * `importActual`) so these behaviors can be asserted directly against
 * controllable spies instead of real network calls.
 */
import { createRef } from 'react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { SettingsPage, type SettingsPageHandle } from '@/components/settings/settings-page'
import { SECRET_KEYS, type SecretKey } from '@/hooks/use-secrets-status'

const saveMock = vi.fn(async (patch: unknown) => {
  void patch
  return {}
})
const saveTouchedKeysMock = vi.fn(async (keys: SecretKey[]) => {
  void keys
  return {}
})

const emptyTouched = () =>
  Object.fromEntries(SECRET_KEYS.map((key) => [key, false])) as Record<SecretKey, boolean>

// Mutable across a test — `useSecretsStatus`'s mock reads this fresh on
// every call, so a test can pre-seed a touched key before rendering.
let mockTouched: Record<SecretKey, boolean> = emptyTouched()

vi.mock('@/hooks/use-settings-form', async () => {
  const actual = await vi.importActual<typeof import('@/hooks/use-settings-form')>('@/hooks/use-settings-form')
  return {
    ...actual,
    useSettingsForm: () => ({
      settings: {},
      setSettings: vi.fn(),
      loading: false,
      saving: false,
      error: null,
      save: saveMock,
    }),
  }
})

vi.mock('@/hooks/use-secrets-status', async () => {
  const actual = await vi.importActual<typeof import('@/hooks/use-secrets-status')>('@/hooks/use-secrets-status')
  return {
    ...actual,
    useSecretsStatus: () => ({
      status: Object.fromEntries(actual.SECRET_KEYS.map((key: SecretKey) => [key, false])),
      values: Object.fromEntries(actual.SECRET_KEYS.map((key: SecretKey) => [key, ''])),
      touched: mockTouched,
      loading: false,
      error: null,
      setFieldValue: vi.fn(),
      saveTouchedKeys: saveTouchedKeysMock,
      clearKey: vi.fn(),
    }),
  }
})

// The Settings page mounts AlarmTransportsProvider for the whole page (so an
// Alarms-section edit counts toward the page's dirty signal), which means the
// real hook would fire its /api/alarm-transports GET into jsdom. Stub it the
// same way useSettingsForm/useSecretsStatus are stubbed above. `config` is
// built once so its identity is stable across renders.
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

beforeEach(() => {
  mockTouched = emptyTouched()
  saveMock.mockReset().mockResolvedValue({})
  saveTouchedKeysMock.mockReset().mockResolvedValue({})
})

describe('SettingsPage dirty tracking', () => {
  it('reports dirty=false initially, true after a draft field changes, and false again after a successful save', async () => {
    const onDirtyChange = vi.fn()
    render(
      <SettingsPage
        onDirtyChange={onDirtyChange}
      />,
    )

    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
    onDirtyChange.mockClear()

    // Switch to the "Vessel" section (boat-ui) and edit a plain text field —
    // avoids the General section's Base UI Select, which needs pointer-event
    // affordances jsdom doesn't provide.
    fireEvent.click(screen.getByRole('button', { name: 'Vessel' }))
    fireEvent.change(screen.getByLabelText('Vessel prefix'), { target: { value: 'S/V Test' } })

    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))

    onDirtyChange.mockClear()
    fireEvent.click(screen.getByRole('button', { name: /save settings/i }))

    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
    expect(saveMock).toHaveBeenCalledTimes(1)
  })

  it('reports dirty=true when a provider-modal-only secret key is touched, even though its modal is not mounted', () => {
    mockTouched = { ...emptyTouched(), WEATHERKIT_KEY_ID: true }
    const onDirtyChange = vi.fn()
    render(
      <SettingsPage
        onDirtyChange={onDirtyChange}
      />,
    )

    expect(onDirtyChange).toHaveBeenLastCalledWith(true)
    // The WeatherKit provider modal is not open/mounted anywhere in this
    // render (default active section is "General") — proves the dirty
    // signal comes from the full `touched` map, not rendered DOM.
    expect(screen.queryByLabelText(/WeatherKit/i)).not.toBeInTheDocument()
  })

  it('reports dirty=false when a field is changed back to its original saved value (regression: was stuck true)', async () => {
    const onDirtyChange = vi.fn()
    render(
      <SettingsPage
        onDirtyChange={onDirtyChange}
      />,
    )

    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
    onDirtyChange.mockClear()

    // Switch to the "Vessel" section and edit the vessel prefix field
    fireEvent.click(screen.getByRole('button', { name: 'Vessel' }))
    const vesselPrefixInput = screen.getByLabelText('Vessel prefix') as HTMLInputElement

    // Change it to a new value — should become dirty
    fireEvent.change(vesselPrefixInput, { target: { value: 'S/V Test' } })
    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))

    onDirtyChange.mockClear()

    // Change it back to its original value (empty string) — should become clean
    fireEvent.change(vesselPrefixInput, { target: { value: '' } })
    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
  })
})

// anchor.scope_method selects which rode-calculation method the Anchor
// Watch tile uses. Driving the Base UI Select in jsdom needs the trigger
// opened via a plain click, then the target item picked with a
// pointerdown+pointerup+click sequence — a click alone (the way every other
// field in this suite is driven) does not register the selection, which is
// also why the General section's own Select is avoided elsewhere in this
// file.
describe('SettingsPage Anchor scope method', () => {
  it('renders the Scope Method select, marks the form dirty on change, and saves the chosen value', async () => {
    const onDirtyChange = vi.fn()
    render(
      <SettingsPage
        onDirtyChange={onDirtyChange}
      />,
    )

    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
    onDirtyChange.mockClear()

    fireEvent.click(screen.getByRole('button', { name: 'Anchor Watch' }))

    const trigger = screen.getByLabelText('Scope method')
    expect(trigger).toBeInTheDocument()

    fireEvent.click(trigger)
    const catenaryOption = await screen.findByText(/Catenary/)
    fireEvent.pointerDown(catenaryOption, { pointerId: 1, pointerType: 'mouse', button: 0 })
    fireEvent.pointerUp(catenaryOption, { pointerId: 1, pointerType: 'mouse', button: 0 })
    fireEvent.click(catenaryOption)

    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))

    fireEvent.click(screen.getByRole('button', { name: /save settings/i }))

    await waitFor(() => expect(saveMock).toHaveBeenCalledTimes(1))
    const patch = saveMock.mock.calls[0][0] as { anchor?: { scope_method?: string } }
    expect(patch.anchor?.scope_method).toBe('catenary')
  })
})

// ADR 0099: the auto-raise watcher's setting moved off a per-browser
// localStorage toggle and onto this same Anchor Watch panel's settings form,
// following gps_from_bow_m's own path exactly (draft -> patch -> save()).
describe('SettingsPage Anchor auto-raise setting', () => {
  it('defaults the switch on, and saving after turning it off patches auto_raise_on_motoring:false', async () => {
    const onDirtyChange = vi.fn()
    render(
      <SettingsPage
        onDirtyChange={onDirtyChange}
      />,
    )

    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(false))
    onDirtyChange.mockClear()

    fireEvent.click(screen.getByRole('button', { name: 'Anchor Watch' }))

    const toggle = screen.getByRole('switch')
    expect(toggle).toHaveAttribute('data-checked')

    fireEvent.click(toggle)
    await waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))
    expect(toggle).toHaveAttribute('data-unchecked')

    fireEvent.click(screen.getByRole('button', { name: /save settings/i }))

    await waitFor(() => expect(saveMock).toHaveBeenCalledTimes(1))
    const patch = saveMock.mock.calls[0][0] as { anchor?: { auto_raise_on_motoring?: boolean } }
    expect(patch.anchor?.auto_raise_on_motoring).toBe(false)
  })
})

describe('SettingsPage imperative save handle', () => {
  it('saves the settings patch and ALL touched secrets (the full SECRET_KEYS set, not just the inline-section subset)', async () => {
    const ref = createRef<SettingsPageHandle>()
    render(
      <SettingsPage ref={ref} />,
    )

    await ref.current!.save()

    expect(saveMock).toHaveBeenCalledTimes(1)
    expect(saveTouchedKeysMock).toHaveBeenCalledWith(SECRET_KEYS)
    expect(saveTouchedKeysMock.mock.calls[0][0]).toContain('WEATHERKIT_KEY_ID')
  })

  it('rejects when the underlying save fails', async () => {
    saveMock.mockRejectedValueOnce(new Error('boom'))
    const ref = createRef<SettingsPageHandle>()
    render(
      <SettingsPage ref={ref} />,
    )

    await expect(ref.current!.save()).rejects.toThrow('boom')
  })
})

// ADR 0095: one Manual button per section, calling back with that section's
// manual target rather than each section rendering its own.
describe('SettingsPage Manual button', () => {
  it('calls onOpenManual with the active section\'s manual target', () => {
    const onOpenManual = vi.fn()
    render(
      <SettingsPage
        onOpenManual={onOpenManual}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Alarms' }))
    fireEvent.click(screen.getByRole('button', { name: 'Open help for this section' }))

    expect(onOpenManual).toHaveBeenCalledWith({ page: 'features/alarms', heading: 'Getting told' })
  })

  it('renders no Manual button when onOpenManual is not passed - existing callers are untouched', () => {
    render(<SettingsPage />)

    expect(screen.queryByRole('button', { name: 'Open help for this section' })).not.toBeInTheDocument()
  })
})
