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
    expect(limits?.ahead).toEqual({ wind_kn_no_go: null, wave_m_no_go: null })
    expect(limits?.astern).toEqual({ wind_kn_no_go: null, wave_m_no_go: null })
    expect(limits?.wind_against_tide_offset_kn).toBeNull()
    expect(limits?.daylight_arrival).toBe(false)
  })

  it('typing a value saves it as a number and clearing it again saves null', () => {
    const { latest } = renderSection()
    const input = screen.getByLabelText('Ahead no-go wind (kn)')
    fireEvent.change(input, { target: { value: '15' } })
    expect(buildRegularSettingsPatch(latest()).assistant?.passage_limits?.ahead).toEqual({ wind_kn_no_go: 15, wave_m_no_go: null })
    fireEvent.change(input, { target: { value: '' } })
    expect(buildRegularSettingsPatch(latest()).assistant?.passage_limits?.ahead).toEqual({ wind_kn_no_go: null, wave_m_no_go: null })
    expect(draftsEqual(latest(), { ...initialRegularSettingsDraft, assistantEnabled: true })).toBe(true)
    vi.unstubAllGlobals()
  })

  it('hydrates stored limits and leaves absent ones blank', () => {
    const draft = hydrateDraftFromSettings({
      assistant: { passage_limits: { abeam: { wind_kn_no_go: 22, wave_m_no_go: 2 }, wind_against_tide_offset_kn: 5, daylight_arrival: true } },
    })
    expect(draft.assistantPassageLimits.abeam.wind).toBe('22')
    expect(draft.assistantPassageLimits.abeam.wave).toBe('2')
    expect(draft.assistantPassageLimits.ahead.wind).toBe('')
    expect(draft.assistantPassageLimits.tideOffset).toBe('5')
    expect(draft.assistantPassageLimits.daylightArrival).toBe(true)
    expect(passageLimitErrors(hydrateDraftFromSettings({}))).toEqual({})
  })

  it('has no marginal or gust boxes: only no-go wind and wave per sector', () => {
    renderSection()
    expect(screen.queryByLabelText(/marginal/i)).toBeNull()
    expect(screen.queryByLabelText(/gust/i)).toBeNull()
    expect(screen.getByLabelText('Astern no-go wave height (m)')).toBeInTheDocument()
    vi.unstubAllGlobals()
  })

  it('rejects a negative value', () => {
    const { latest } = renderSection()
    fireEvent.change(screen.getByLabelText('Astern no-go wave height (m)'), { target: { value: '-1' } })
    expect(screen.getByText('Astern wave height: enter 0 or more')).toBeInTheDocument()
    expect(Object.keys(passageLimitErrors(latest()))).toHaveLength(1)
    vi.unstubAllGlobals()
  })

  it('the wind against tide reduction must be above 0 and below every no-go wind', () => {
    const { latest } = renderSection()
    const offset = screen.getByLabelText('Reduce wind limits by (kn)')
    fireEvent.change(offset, { target: { value: '0' } })
    expect(screen.getByText('Wind against tide: enter more than 0')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Ahead no-go wind (kn)'), { target: { value: '15' } })
    fireEvent.change(offset, { target: { value: '15' } })
    expect(screen.getByText('Wind against tide: must be less than the ahead no-go wind (15 kn)')).toBeInTheDocument()
    fireEvent.change(offset, { target: { value: '5' } })
    expect(passageLimitErrors(latest())).toEqual({})
    expect(buildRegularSettingsPatch(latest()).assistant?.passage_limits?.wind_against_tide_offset_kn).toBe(5)
    vi.unstubAllGlobals()
  })

  it('has a Passage limits group with the control id the setup step lands on', () => {
    renderSection()
    expect(document.getElementById('passage-limits')).not.toBeNull()
    expect(screen.getByLabelText('Daylight arrival')).toBeInTheDocument()
    expect(screen.getByLabelText('Reduce wind limits by (kn)')).toBeInTheDocument()
    vi.unstubAllGlobals()
  })

  it('reads limits as text so unparseable input is visible: "15kn" is an error and blocks Save', () => {
    const { latest } = renderSection()
    const input = screen.getByLabelText('Ahead no-go wind (kn)')
    expect(input).toHaveAttribute('type', 'text')
    expect(input).toHaveAttribute('inputmode', 'decimal')
    fireEvent.change(input, { target: { value: '15kn' } })
    expect(screen.getByText(/enter a number/i)).toBeInTheDocument()
    expect(Object.keys(passageLimitErrors(latest()))).toHaveLength(1)
    vi.unstubAllGlobals()
  })

  it('accepts one comma as the decimal separator and blank as null', () => {
    const { latest } = renderSection()
    const input = screen.getByLabelText('Ahead no-go wind (kn)')
    fireEvent.change(input, { target: { value: '12,5' } })
    expect(passageLimitErrors(latest())).toEqual({})
    expect(buildRegularSettingsPatch(latest()).assistant?.passage_limits?.ahead?.wind_kn_no_go).toBe(12.5)
    fireEvent.change(input, { target: { value: '1,2,3' } })
    expect(Object.keys(passageLimitErrors(latest()))).toHaveLength(1)
    fireEvent.change(input, { target: { value: '' } })
    expect(passageLimitErrors(latest())).toEqual({})
    expect(buildRegularSettingsPatch(latest()).assistant?.passage_limits?.ahead?.wind_kn_no_go).toBeNull()
    vi.unstubAllGlobals()
  })

  it('explains how marginal and gusts are worked out', () => {
    renderSection()
    expect(screen.getByText(/Marginal is 70% of no-go/)).toBeInTheDocument()
    expect(screen.getByText(/no-go wind plus 10 kn/)).toBeInTheDocument()
    expect(screen.getByText(/Each sector needs a no-go wind before Mate will assess a leg in it/)).toBeInTheDocument()
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
