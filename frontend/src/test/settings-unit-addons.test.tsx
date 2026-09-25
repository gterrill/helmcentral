import { describe, it, expect } from 'vitest'
import { useState } from 'react'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { AnchorWatchOptionsSection } from '@/components/settings/sections/anchor-watch-options-section'
import { initialRegularSettingsDraft, type RegularSettingsDraft } from '@/components/settings/settings-draft'

// These sections gained unit-of-measure addons (an InputGroup wrapping each
// Input with a trailing "m" / "mm" / "m²" symbol) so the unit lives
// next to the value instead of only in the label text. These tests pin: the
// new aria-labels, the addon symbol rendering as text, that typing still
// reaches onChange/the draft (InputGroupInput must not swallow events), and
// that clicking an addon focuses its input (the upstream click-to-focus
// behaviour InputGroupAddon provides).

function renderAnchorSection(overrides: Partial<RegularSettingsDraft> = {}) {
  const draftStates: RegularSettingsDraft[] = []

  function Harness() {
    const [draft, setDraft] = useState<RegularSettingsDraft>({ ...initialRegularSettingsDraft, ...overrides })
    draftStates.push(draft)
    return (
      <AnchorWatchOptionsSection
        draft={draft}
        onChange={(patch) => setDraft((previous) => ({ ...previous, ...patch }))}
      />
    )
  }

  render(<Harness />)
  return { latestDraft: () => draftStates[draftStates.length - 1] }
}

describe('settings unit addons', () => {
  describe('AnchorWatchOptionsSection', () => {
    it('exposes the anchor fields by their new aria-labels with unit addons rendered as text', () => {
      renderAnchorSection()

      const bowRoller = screen.getByLabelText('Bow roller height in metres')
      expect(within(bowRoller.closest('[data-slot="input-group"]') as HTMLElement).getByText('m')).toBeInTheDocument()

      const gpsFromBow = screen.getByLabelText('GPS antenna distance aft of bow roller in metres')
      expect(within(gpsFromBow.closest('[data-slot="input-group"]') as HTMLElement).getByText('m')).toBeInTheDocument()

      const loa = screen.getByLabelText('Length overall in metres')
      expect(within(loa.closest('[data-slot="input-group"]') as HTMLElement).getByText('m')).toBeInTheDocument()

      const chainSize = screen.getByLabelText('Chain size in millimetres')
      expect(within(chainSize.closest('[data-slot="input-group"]') as HTMLElement).getByText('mm')).toBeInTheDocument()

      const chainOnboard = screen.getByLabelText('Chain onboard length in metres')
      expect(within(chainOnboard.closest('[data-slot="input-group"]') as HTMLElement).getByText('m')).toBeInTheDocument()

      const windage = screen.getByLabelText('Windage area in square metres')
      expect(within(windage.closest('[data-slot="input-group"]') as HTMLElement).getByText('m²')).toBeInTheDocument()

      // "m" appears four times on this section (bow roller, GPS-from-bow, LOA,
      // chain onboard), hence scoping each lookup above rather than a single
      // page-wide getByText.
      expect(screen.getAllByText('m')).toHaveLength(4)
    })

    it('still fires onChange with the raw string value when typing into an addon-wrapped input', () => {
      const { latestDraft } = renderAnchorSection()

      const bowRoller = screen.getByLabelText('Bow roller height in metres')
      fireEvent.change(bowRoller, { target: { value: '2.1' } })

      expect(latestDraft().bowRollerHeightM).toBe('2.1')
    })

    it('focuses the input when its unit addon is clicked', () => {
      renderAnchorSection()

      const windage = screen.getByLabelText('Windage area in square metres')
      const addon = windage.closest('[data-slot="input-group"]')!.querySelector('[data-slot="input-group-addon"]') as HTMLElement
      fireEvent.click(addon)

      expect(windage).toHaveFocus()
    })
  })
})
