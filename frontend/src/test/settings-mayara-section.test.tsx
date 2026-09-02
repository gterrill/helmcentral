import { describe, it, expect } from 'vitest'
import { useState } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { MayaraSection } from '@/components/settings/sections/mayara-section'
import {
  buildRegularSettingsPatch,
  draftsEqual,
  hydrateDraftFromSettings,
  initialRegularSettingsDraft,
  type RegularSettingsDraft,
} from '@/components/settings/settings-draft'
import type { SettingsPayload } from '@/hooks/use-settings-form'

// Phase 2 of the radar overlay plan: the Mayara settings section and the
// settings-draft plumbing that carries its two fields through hydrate/save.
// No toggle, no overlay, no WebSocket yet — just the address getting to and
// from /api/settings intact, including the blank case, since a
// plausible-looking default host would fail confusingly rather than
// obviously.

function renderSection(overrides: Partial<RegularSettingsDraft> = {}) {
  const draftStates: RegularSettingsDraft[] = []

  function Harness() {
    const [draft, setDraft] = useState<RegularSettingsDraft>({ ...initialRegularSettingsDraft, ...overrides })
    draftStates.push(draft)
    return (
      <MayaraSection
        draft={draft}
        onChange={(patch) => setDraft((previous) => ({ ...previous, ...patch }))}
      />
    )
  }

  render(<Harness />)
  return { latestDraft: () => draftStates[draftStates.length - 1] }
}

describe('MayaraSection', () => {
  it('renders address and port inputs addressable by aria-label', () => {
    renderSection()

    expect(screen.getByLabelText('Mayara address')).toBeInTheDocument()
    expect(screen.getByLabelText('Mayara port')).toBeInTheDocument()
  })

  // This is the draftsEqual regression guard: forgetting to add the mayara
  // fields to draftsEqual leaves the dirty indicator silently broken, with
  // no other test in the suite that would catch it.
  it('typing an address marks the form dirty', () => {
    const { latestDraft } = renderSection()
    expect(draftsEqual(latestDraft(), initialRegularSettingsDraft)).toBe(true)

    fireEvent.change(screen.getByLabelText('Mayara address'), { target: { value: '192.168.50.81' } })

    expect(draftsEqual(latestDraft(), initialRegularSettingsDraft)).toBe(false)
  })

  it('changing the port marks the form dirty', () => {
    const { latestDraft } = renderSection()
    expect(draftsEqual(latestDraft(), initialRegularSettingsDraft)).toBe(true)

    fireEvent.change(screen.getByLabelText('Mayara port'), { target: { value: '6503' } })

    expect(draftsEqual(latestDraft(), initialRegularSettingsDraft)).toBe(false)
  })
})

describe('mayara settings-draft plumbing', () => {
  it('buildRegularSettingsPatch emits mayara with the typed address and port', () => {
    const draft: RegularSettingsDraft = {
      ...initialRegularSettingsDraft,
      mayaraAddress: '192.168.50.81',
      mayaraPort: '6503',
    }

    const patch = buildRegularSettingsPatch(draft)

    expect(patch.mayara).toEqual({ address: '192.168.50.81', port: 6503 })
  })

  it('hydrateDraftFromSettings round-trips an address and port', () => {
    const settings: SettingsPayload = { mayara: { address: '192.168.50.81', port: 6503 } }

    const draft = hydrateDraftFromSettings(settings)

    expect(draft.mayaraAddress).toBe('192.168.50.81')
    expect(draft.mayaraPort).toBe('6503')
  })

  // The address is blank whenever the operator hasn't configured a radar
  // server. It must round-trip as blank rather than acquiring a default
  // host: a wrong-but-plausible address fails confusingly, a blank one fails
  // obviously.
  it('hydrates and saves a blank address as blank, acquiring no default host', () => {
    const settings: SettingsPayload = { mayara: { address: '', port: 6502 } }

    const draft = hydrateDraftFromSettings(settings)
    expect(draft.mayaraAddress).toBe('')

    const patch = buildRegularSettingsPatch(draft)
    expect(patch.mayara?.address).toBe('')
  })

  it('hydrates an absent mayara block to the blank default', () => {
    const draft = hydrateDraftFromSettings({})

    expect(draft.mayaraAddress).toBe('')
    expect(draft.mayaraPort).toBe('6502')
  })
})
