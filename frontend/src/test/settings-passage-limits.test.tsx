import { describe, it, expect, vi } from 'vitest'
import { useState } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { peekPendingSetupFocus, requestSetupFocus } from '@/lib/setup-status'
import { AssistantSection } from '@/components/settings/sections/assistant-section'
import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'
import {
  buildRegularSettingsPatch,
  draftsEqual,
  hydrateDraftFromSettings,
  initialRegularSettingsDraft,
  passageLimitErrors,
  type RegularSettingsDraft,
} from '@/components/settings/settings-draft'

function renderSection(overrides: Partial<RegularSettingsDraft> = {}) {
  const states: RegularSettingsDraft[] = []
  function Harness() {
    const [draft, setDraft] = useState<RegularSettingsDraft>({ ...initialRegularSettingsDraft, assistantEnabled: true, ...overrides })
    states.push(draft)
    return (
      <SecretsStatusProvider>
        <AssistantSection draft={draft} onChange={(patch) => setDraft((p) => ({ ...p, ...patch }))} />
      </SecretsStatusProvider>
    )
  }
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
  render(<Harness />)
  return { latest: () => states[states.length - 1] }
}

describe('Passage limits settings', () => {
  it('saves every blank limit as null, never 0', () => {
    const limits = buildRegularSettingsPatch(initialRegularSettingsDraft).assistant?.passage_limits
    expect(limits?.ahead?.wind_kn).toEqual({ marginal: null, no_go: null })
    expect(limits?.astern?.wave_m).toEqual({ marginal: null, no_go: null })
    expect(limits?.wind_against_tide?.wind_kn).toEqual({ marginal: null, no_go: null })
    expect(limits?.daylight_arrival).toBe(false)
  })

  it('typing a value saves it as a number and clearing it again saves null', () => {
    const { latest } = renderSection()
    const input = screen.getByLabelText('Ahead wind no-go (kn)')
    fireEvent.change(input, { target: { value: '15' } })
    expect(buildRegularSettingsPatch(latest()).assistant?.passage_limits?.ahead?.wind_kn).toEqual({ marginal: null, no_go: 15 })
    fireEvent.change(input, { target: { value: '' } })
    expect(buildRegularSettingsPatch(latest()).assistant?.passage_limits?.ahead?.wind_kn).toEqual({ marginal: null, no_go: null })
    expect(draftsEqual(latest(), { ...initialRegularSettingsDraft, assistantEnabled: true })).toBe(true)
    vi.unstubAllGlobals()
  })

  it('hydrates stored limits and leaves absent ones blank', () => {
    const draft = hydrateDraftFromSettings({
      assistant: { passage_limits: { abeam: { gust_kn: { marginal: 20, no_go: 25 } }, wind_against_tide: { wind_kn: { no_go: 12 } }, daylight_arrival: true } },
    })
    expect(draft.assistantPassageLimits.abeam.gust.marginal).toBe('20')
    expect(draft.assistantPassageLimits.abeam.gust.noGo).toBe('25')
    expect(draft.assistantPassageLimits.ahead.wind.noGo).toBe('')
    expect(draft.assistantPassageLimits.tide.noGo).toBe('12')
    expect(draft.assistantPassageLimits.daylightArrival).toBe(true)
    expect(passageLimitErrors(hydrateDraftFromSettings({}))).toEqual({})
  })

  it('shows an inline error when marginal is not below no-go, and reports it for the Save bar', () => {
    const { latest } = renderSection()
    fireEvent.change(screen.getByLabelText('Abeam gust marginal (kn)'), { target: { value: '25' } })
    fireEvent.change(screen.getByLabelText('Abeam gust no-go (kn)'), { target: { value: '20' } })
    expect(screen.getByText('Abeam gust: marginal must be below no-go')).toBeInTheDocument()
    expect(Object.keys(passageLimitErrors(latest()))).not.toHaveLength(0)
    fireEvent.change(screen.getByLabelText('Abeam gust marginal (kn)'), { target: { value: '15' } })
    expect(screen.queryByText('Abeam gust: marginal must be below no-go')).toBeNull()
    expect(passageLimitErrors(latest())).toEqual({})
    vi.unstubAllGlobals()
  })

  it('rejects a negative value', () => {
    const { latest } = renderSection()
    fireEvent.change(screen.getByLabelText('Astern wave no-go (m)'), { target: { value: '-1' } })
    expect(screen.getByText('Astern wave height: enter 0 or more')).toBeInTheDocument()
    expect(Object.keys(passageLimitErrors(latest()))).toHaveLength(1)
    vi.unstubAllGlobals()
  })

  it('has a Passage limits group with the control id the setup step lands on', () => {
    renderSection()
    expect(document.getElementById('passage-limits')).not.toBeNull()
    expect(screen.getByLabelText('Daylight arrival')).toBeInTheDocument()
    expect(screen.getByLabelText('Wind against tide no-go (kn)')).toBeInTheDocument()
    vi.unstubAllGlobals()
  })

  it('reads limits as text so unparseable input is visible: "15kn" is an error and blocks Save', () => {
    const { latest } = renderSection()
    const input = screen.getByLabelText('Ahead wind no-go (kn)')
    expect(input).toHaveAttribute('type', 'text')
    expect(input).toHaveAttribute('inputmode', 'decimal')
    fireEvent.change(input, { target: { value: '15kn' } })
    expect(screen.getByText(/enter a number/i)).toBeInTheDocument()
    expect(Object.keys(passageLimitErrors(latest()))).toHaveLength(1)
    vi.unstubAllGlobals()
  })

  it('accepts one comma as the decimal separator and blank as null', () => {
    const { latest } = renderSection()
    const input = screen.getByLabelText('Ahead wind no-go (kn)')
    fireEvent.change(input, { target: { value: '12,5' } })
    expect(passageLimitErrors(latest())).toEqual({})
    expect(buildRegularSettingsPatch(latest()).assistant?.passage_limits?.ahead?.wind_kn).toEqual({ marginal: null, no_go: 12.5 })
    fireEvent.change(input, { target: { value: '1,2,3' } })
    expect(Object.keys(passageLimitErrors(latest()))).toHaveLength(1)
    fireEvent.change(input, { target: { value: '' } })
    expect(passageLimitErrors(latest())).toEqual({})
    expect(buildRegularSettingsPatch(latest()).assistant?.passage_limits?.ahead?.wind_kn).toEqual({ marginal: null, no_go: null })
    vi.unstubAllGlobals()
  })

  it('explains what a blank limit means', () => {
    renderSection()
    expect(screen.getByText(/Each sector needs at least a no-go wind limit before Mate will assess a leg in it/)).toBeInTheDocument()
    expect(screen.getByText(/Marginal must be below no-go/)).toBeInTheDocument()
    vi.unstubAllGlobals()
  })

  it('focuses #passage-limits when a setup focus request is waiting as the section mounts', async () => {
    requestSetupFocus('passage-limits')
    renderSection()
    await waitFor(() => expect(document.activeElement).toBe(document.getElementById('passage-limits')))
    expect(peekPendingSetupFocus()).toBeNull()
    vi.unstubAllGlobals()
  })
})
