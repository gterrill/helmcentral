import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PassageFeltRow } from '@/components/passages/passage-stats'
import { spellLabel } from '@/components/passages/motion-strip'
import type { FeltSpell, PassageFelt, SeaTrain, SpellSea } from '@/lib/passages'

const swellAndChop: SpellSea = {
  swell: { height_m: 0.8, period_s: 9, from_deg: 270, direction: 'on the port beam' },
  wind_wave: { height_m: 0.6, period_s: 4, from_deg: 300, direction: 'on the port bow' },
}

const flatSea: SpellSea = { swell: null, wind_wave: null }

function spell(over: Partial<FeltSpell> = {}): FeltSpell {
  return { start: 1_700_000_000, end: 1_700_000_900, sway_deg: 1.9, band: 'lively', sea: swellAndChop, ...over }
}

describe('the sea in a spell\'s label', () => {
  it('names the swell and the wind wave, each with its height, period and where it comes from', () => {
    expect(spellLabel(spell(), 'UTC')).toMatch(
      /^Lively spell, 1\.9° sway, from .+\. Sea: swell 0\.8 m 9 s on the port beam, wind wave 0\.6 m 4 s on the port bow$/,
    )
  })

  it('leaves out a system the model has none of', () => {
    const label = spellLabel(spell({ sea: { swell: swellAndChop.swell, wind_wave: null } }), 'UTC')
    expect(label).toMatch(/Sea: swell 0\.8 m 9 s on the port beam$/)
    expect(label).not.toMatch(/wind wave/)
  })

  it('says the sea was flat, and gives the server\'s reason when there is no sea state', () => {
    expect(spellLabel(spell({ sea: flatSea }), 'UTC')).toMatch(/Sea: no swell or wind wave$/)
    expect(spellLabel(spell({ sea: { swell: null, wind_wave: null, reason: 'GFS-Wave has no value at this cell.' } }), 'UTC'))
      .toMatch(/Sea: GFS-Wave has no value at this cell\.$/)
  })

  it('keeps the old label for a server that sends no sea', () => {
    const label = spellLabel(spell({ sea: undefined }), 'UTC')
    expect(label).toBe('Lively spell, 1.9° sway, from 14 Nov 10.13 PM UTC')
    expect(label).not.toMatch(/Sea/)
  })

  it('still says the band is not learned yet', () => {
    expect(spellLabel(spell({ band: null }), 'UTC')).toMatch(/^Spell, 1\.9° sway, from .+, band not learned yet\. Sea: /)
  })
})

describe('the fetch check on the wind wave in a spell\'s label', () => {
  const withWind = (wind_wave: SeaTrain): FeltSpell => spell({ sea: { swell: null, wind_wave } })
  const bow = { from_deg: 300, direction: 'on the port bow' }

  it('shows the capped figures and the server\'s note on what they were limited from', () => {
    const label = spellLabel(withWind({ height_m: 0.3, period_s: 2, ...bow, fetch: { check: 'capped' }, note: 'fetch-limited from 0.9 m: 3.1 nm fetch in 14 kn' }), 'UTC')
    expect(label).toMatch(/Sea: wind wave 0\.3 m 2 s on the port bow \(fetch-limited from 0\.9 m: 3\.1 nm fetch in 14 kn\)$/)
  })

  it('shows the note of a wind wave under the cap', () => {
    const label = spellLabel(withWind({ height_m: 0.3, period_s: 4, ...bow, fetch: { check: 'within' }, note: 'within the 0.6 m fetch limit' }), 'UTC')
    expect(label).toMatch(/wind wave 0\.3 m 4 s on the port bow \(within the 0\.6 m fetch limit\)$/)
  })

  it('shows the note of open water upwind', () => {
    const label = spellLabel(withWind({ height_m: 0.9, period_s: 5, ...bow, fetch: { check: 'open' }, note: 'open water upwind' }), 'UTC')
    expect(label).toMatch(/wind wave 0\.9 m 5 s on the port bow \(open water upwind\)$/)
  })

  it('labels the model figure as not checked, with the server\'s reason', () => {
    const label = spellLabel(withWind({ height_m: 0.9, period_s: 5, ...bow, fetch: { check: 'unchecked', reason: 'the Offline coastline is off' }, note: 'not checked against fetch: the Offline coastline is off' }), 'UTC')
    expect(label).toMatch(/wind wave 0\.9 m 5 s on the port bow \(not checked against fetch: the Offline coastline is off\)$/)
  })

  it('says swell is not judged in enclosed water instead of giving its figures', () => {
    const label = spellLabel(spell({ sea: { swell: { height_m: 0.8, period_s: 9, from_deg: 135, direction: 'astern', enclosed: true, note: 'swell not judged in enclosed water' }, wind_wave: null } }), 'UTC')
    expect(label).toMatch(/Sea: swell not judged in enclosed water$/)
    expect(label).not.toMatch(/0\.8 m/)
  })

  it('keeps the plain wording for a server that sends no check', () => {
    expect(spellLabel(spell(), 'UTC')).toMatch(/wind wave 0\.6 m 4 s on the port bow$/)
  })
})

function felt(over: Partial<PassageFelt> = {}): PassageFelt {
  return {
    deg: 2.4, band: 'lively', bands: { learned: true, rated_count: 7, lively_from_deg: 1.8, rough_from_deg: null },
    spells: [], snap: { value: null, at: null, reason: 'x' }, pounding: { value_g: null, at: null, reason: 'y' },
    ...over,
  }
}

describe('the sea line in the Felt row', () => {
  it('names the largest swell and wind wave and when', () => {
    render(<PassageFeltRow rating="" timeZone="UTC" felt={felt({
      sea: {
        swell: { height_m: 1.4, period_s: 11, from_deg: 270, at: 1_700_000_000 },
        wind_wave: { height_m: 0.9, period_s: 4, from_deg: 315, at: 1_700_003_600 },
      },
    })} />)
    const line = screen.getByTestId('felt-sea')
    expect(line).toHaveTextContent(/Sea: largest swell 1\.4 m 11 s, .+; largest wind wave 0\.9 m 4 s, .+/)
  })

  it('says how many spells the peaks leave out when only some have a sea', () => {
    render(<PassageFeltRow rating="" timeZone="UTC" felt={felt({
      sea: {
        swell: { height_m: 1.4, period_s: 11, from_deg: 270, at: 1_700_000_000 },
        wind_wave: null,
        partial: 'no sea for 3 of 12 spells',
      },
    })} />)
    expect(screen.getByTestId('felt-sea')).toHaveTextContent(/Sea: largest swell 1\.4 m 11 s, .+ \(no sea for 3 of 12 spells\)$/)
  })

  it('says how many wind waves the peak leaves out for not being checked against fetch', () => {
    render(<PassageFeltRow rating="" timeZone="UTC" felt={felt({
      sea: {
        swell: null,
        wind_wave: { height_m: 0.5, period_s: 4, from_deg: 315, at: 1_700_003_600 },
        partial: 'wind wave not checked against fetch for 1 of 3 spells',
      },
    })} />)
    expect(screen.getByTestId('felt-sea')).toHaveTextContent(/largest wind wave 0\.5 m 4 s, .+ \(wind wave not checked against fetch for 1 of 3 spells\)$/)
  })

  it('gives the server\'s reason when no spell has a sea state', () => {
    render(<PassageFeltRow rating="" timeZone="UTC" felt={felt({ sea: { swell: null, wind_wave: null, reason: "The sea state for this passage hasn't been fetched yet." } })} />)
    expect(screen.getByTestId('felt-sea')).toHaveTextContent("Sea: The sea state for this passage hasn't been fetched yet.")
  })

  it('says a flat sea in words, and draws nothing for a server that sends no sea', () => {
    const { rerender } = render(<PassageFeltRow rating="" timeZone="UTC" felt={felt({ sea: { swell: null, wind_wave: null } })} />)
    expect(screen.getByTestId('felt-sea')).toHaveTextContent('Sea: no swell or wind wave')
    rerender(<PassageFeltRow rating="" timeZone="UTC" felt={felt()} />)
    expect(screen.queryByTestId('felt-sea')).toBeNull()
  })
})
