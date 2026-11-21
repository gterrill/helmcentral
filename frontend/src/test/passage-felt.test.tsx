import { fireEvent, render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { PassageFeltRow } from '@/components/passages/passage-stats'
import type { MotionSpellTick } from '@/components/passages/motion-strip'
import { PassageReplayBar, type PassageReplayBarProps } from '@/components/passages/passage-replay-bar'
import { PassagesPanel } from '@/components/passages/passages-panel'
import type { PassageFelt, PassageSummary } from '@/lib/passages'
import { severityFill } from '@/lib/severity'

vi.mock('@/lib/passages-api', () => ({ passagesApi: {} }))
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

const learned = { learned: true, rated_count: 7, summary: 'Learned from 7 rated passages: Lively from 1.8°.', lively_from_deg: 1.8, rough_from_deg: null }
const unlearned = { learned: false, rated_count: 2, reason: 'Not learned yet: 2 rated passages with a felt figure, and it needs at least 3.', lively_from_deg: null, rough_from_deg: null }

function felt(over: Partial<PassageFelt> = {}): PassageFelt {
  return {
    deg: 2.4, band: 'lively', bands: learned, spells: [],
    snap: { kind: 'sideways_g', value: 0.123, at: 1_700_000_100 },
    pounding: { value_g: 0.84, at: 1_700_000_200 },
    ...over,
  }
}

beforeEach(() => { vi.clearAllMocks() })

describe('the Felt row', () => {
  it('shows the band word beside the operator\'s own rating, and the figure', () => {
    render(<PassageFeltRow felt={felt()} rating="comfortable" />)
    expect(screen.getByTestId('felt-band')).toHaveTextContent('Lively')
    expect(screen.getByTestId('felt-rating')).toHaveTextContent('You rated it Comfortable')
    expect(screen.getByTestId('passage-felt')).toHaveTextContent('2.4° felt')
    expect(screen.getByTestId('passage-felt')).toHaveTextContent('Learned from 7 rated passages: Lively from 1.8°.')
  })

  it('says not rated, and spells out the shouldn\'t have gone rating', () => {
    const { rerender } = render(<PassageFeltRow felt={felt()} rating="" />)
    expect(screen.getByTestId('felt-rating')).toHaveTextContent('Not rated')
    rerender(<PassageFeltRow felt={felt()} rating="shouldnt_have_gone" />)
    expect(screen.getByTestId('felt-rating')).toHaveTextContent("You rated it Shouldn't have gone")
  })

  it('says not learned yet, with the reason, until the bands are learned', () => {
    render(<PassageFeltRow felt={felt({ band: null, bands: unlearned })} rating="" />)
    expect(screen.getByTestId('felt-band')).toHaveTextContent('2.4°')
    expect(screen.getByTestId('passage-felt')).toHaveTextContent('Not learned yet: 2 rated passages')
  })

  it('does not say the figure twice when the headline already shows it', () => {
    render(<PassageFeltRow felt={felt({ band: null, bands: unlearned })} rating="" />)
    expect(screen.getByTestId('felt-band')).toHaveTextContent('2.4°')
    expect(screen.getByTestId('passage-felt').textContent?.match(/2\.4°/g)).toHaveLength(1)
    expect(screen.getByTestId('passage-felt')).not.toHaveTextContent('2.4° felt')
    // the snap still reads on its own, with no stray separator in front
    expect(screen.getByTestId('passage-felt').textContent).not.toMatch(/^\s*·/)
  })

  it('carries the note when the figure is rougher than anything rated', () => {
    render(<PassageFeltRow felt={felt({ band_note: 'rougher than any rated passage' })} rating="lively" />)
    expect(screen.getByTestId('passage-felt')).toHaveTextContent('rougher than any rated passage')
  })

  it('moves the replay to snap and to pounding', () => {
    const onSeek = vi.fn()
    render(<PassageFeltRow felt={felt()} rating="" onSeek={onSeek} />)
    fireEvent.click(screen.getByRole('button', { name: 'Show max snap 0.12 g at the helm on the replay' }))
    fireEvent.click(screen.getByRole('button', { name: 'Show max pounding 0.84 g on the replay' }))
    expect(onSeek.mock.calls).toEqual([[1_700_000_100], [1_700_000_200]])
  })

  it('shows roll acceleration when the helm height is not set, and says what is missing', () => {
    render(<PassageFeltRow
      felt={felt({ snap: { kind: 'roll_accel', value: 91.4, at: 1_700_000_300, note: "Helm height isn't set. Enter it under Settings, Vessel." } })}
      rating="" onSeek={vi.fn()}
    />)
    expect(screen.getByRole('button', { name: 'Show max snap 91°/s² roll acceleration on the replay' })).toBeInTheDocument()
    expect(screen.getByText(/Helm height isn't set/)).toBeInTheDocument()
  })

  it('gives the reason for a figure that was not recorded, never a number', () => {
    render(<PassageFeltRow
      felt={felt({
        snap: { value: null, at: null, reason: "This passage was recorded before 10 Hz capture, so snap and pounding aren't available." },
        pounding: { value_g: null, at: null, reason: "This passage was recorded before 10 Hz capture, so snap and pounding aren't available." },
      })}
      rating="" onSeek={vi.fn()}
    />)
    expect(screen.getAllByText(/before 10 Hz capture/)).toHaveLength(2)
    expect(screen.queryByRole('button', { name: /Show max snap/ })).toBeNull()
    expect(screen.queryByRole('button', { name: /Show max pounding/ })).toBeNull()
  })

  it('says why there is no felt figure at all', () => {
    render(<PassageFeltRow felt={felt({ deg: null, band: null, reason: 'No motion was recorded on this passage.', snap: { value: null, at: null }, pounding: { value_g: null, at: null } })} rating="" />)
    expect(screen.getByTestId('felt-band')).toHaveTextContent('--')
    expect(screen.getByText('No motion was recorded on this passage.')).toBeInTheDocument()
  })

  it('shows only the server\'s reasons and keeps no wording of its own for a missing one', () => {
    render(<PassageFeltRow felt={felt({ deg: null, band: null, snap: { value: null, at: null }, pounding: { value_g: null, at: null } })} rating="" />)
    expect(screen.queryByText(/No felt figure for this passage/)).toBeNull()
    expect(screen.queryByText(/not recorded/i)).toBeNull()
  })

  it('still shows snap and pounding, and their reasons, when there is no felt figure', () => {
    const onSeek = vi.fn()
    const { rerender } = render(<PassageFeltRow
      felt={felt({ deg: null, band: null, reason: 'Only roll was recorded on this passage; the felt figure needs roll and pitch.' })}
      rating="" onSeek={onSeek}
    />)
    fireEvent.click(screen.getByRole('button', { name: 'Show max snap 0.12 g at the helm on the replay' }))
    fireEvent.click(screen.getByRole('button', { name: 'Show max pounding 0.84 g on the replay' }))
    expect(onSeek.mock.calls).toEqual([[1_700_000_100], [1_700_000_200]])
    expect(screen.getByText(/Only roll was recorded/)).toBeInTheDocument()

    rerender(<PassageFeltRow
      felt={felt({
        deg: null, band: null, reason: 'No felt figure.',
        snap: { value: null, at: null, reason: 'No snap reason.' },
        pounding: { value_g: null, at: null, reason: 'No pounding reason.' },
      })}
      rating=""
    />)
    expect(screen.getByText('Snap: No snap reason.')).toBeInTheDocument()
    expect(screen.getByText('Pounding: No pounding reason.')).toBeInTheDocument()
  })

  it('shows a snap or pounding value as plain text when it has no moment', () => {
    render(<PassageFeltRow
      felt={felt({ snap: { kind: 'sideways_g', value: 0.123, at: null }, pounding: { value_g: 0.84, at: null } })}
      rating="" onSeek={vi.fn()}
    />)
    expect(screen.getByTestId('passage-felt')).toHaveTextContent('Snap 0.12 g at the helm')
    expect(screen.getByTestId('passage-felt')).toHaveTextContent('Pounding 0.84 g')
    expect(screen.queryByRole('button', { name: /Show max/ })).toBeNull()
    expect(screen.queryByText(/Snap:/)).toBeNull()
  })

  it('draws nothing for a server that sends no felt figures', () => {
    const { container } = render(<PassageFeltRow felt={undefined} rating="" />)
    expect(container).toBeEmptyDOMElement()
  })
})

function bar(over: Partial<PassageReplayBarProps> = {}) {
  const props: PassageReplayBarProps = {
    clock: 0, total: 3600, playing: false, readout: '--', speed: '15s', insetOn: true, disabled: false,
    onToggle: () => {}, onScrub: () => {}, onSpeed: () => {}, onInsetChange: () => {}, ...over,
  }
  render(<PassageReplayBar {...props} />)
}

const spells: MotionSpellTick[] = [
  { id: 'p:0', from: 0, to: 0.25, clock: 0, band: 'comfortable', label: 'Comfortable spell, 0.8° sway, from Thu 14 Oct 9.00 AM' },
  { id: 'p:1', from: 0.25, to: 0.5, clock: 900, band: 'lively', label: 'Lively spell, 2.1° sway, from Thu 14 Oct 9.15 AM' },
  { id: 'p:2', from: 0.5, to: 0.75, clock: 1800, band: 'rough', label: 'Rough spell, 4.0° sway, from Thu 14 Oct 9.30 AM' },
  { id: 'p:3', from: 0.75, to: 1, clock: 2700, band: null, label: 'Spell, 1.0° sway, from Thu 14 Oct 9.45 AM, band not learned yet' },
]

/** What the browser makes of a colour, so a test can compare it with the shared ladder's. */
const asPainted = (colour: string) => {
  const probe = document.createElement('div')
  probe.style.backgroundColor = colour
  return probe.style.backgroundColor
}
const painted = (el: HTMLElement) => (el.firstElementChild as HTMLElement).style.backgroundColor

describe('the motion strip', () => {
  it('draws one stretch per spell, coloured from the shared severity ladder and neutral when not learned', () => {
    bar({ motionStrip: spells, onMotionSeek: vi.fn() })
    const strip = screen.getByTestId('motion-strip')
    const buttons = within(strip).getAllByRole('button')
    expect(buttons).toHaveLength(4)
    expect(painted(buttons[0])).toBe(asPainted(severityFill('normal')))
    expect(painted(buttons[1])).toBe(asPainted(severityFill('warn')))
    expect(painted(buttons[2])).toBe(asPainted(severityFill('alarm')))
    expect(painted(buttons[3])).toBe(asPainted(severityFill(null)))
    expect(new Set(buttons.map(painted)).size).toBe(4)
    expect(buttons[1].style.left).toBe('25%')
    expect(buttons[1].style.width).toBe('25%')
  })

  it('names the band in words, so colour is not the only signal', () => {
    bar({ motionStrip: spells, onMotionSeek: vi.fn() })
    expect(screen.getByRole('button', { name: /^Lively spell, 2.1° sway/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /band not learned yet/ })).toBeInTheDocument()
  })

  it('moves the replay to the spell\'s own clock when it is tapped', () => {
    const onMotionSeek = vi.fn()
    bar({ motionStrip: spells, onMotionSeek })
    fireEvent.click(screen.getByRole('button', { name: /^Rough spell/ }))
    expect(onMotionSeek).toHaveBeenCalledWith(1800)
  })

  it('is left out when there are no spells', () => {
    bar({ motionStrip: [] })
    expect(screen.queryByTestId('motion-strip')).toBeNull()
  })

  it('does not take the scrubber\'s pointer: no drag handling, and the scrubber stays usable', () => {
    bar({ motionStrip: spells, onMotionSeek: vi.fn() })
    const strip = screen.getByTestId('motion-strip')
    expect(strip.className).not.toContain('touch-none')
    expect(screen.getByLabelText('Replay position')).toBeEnabled()
  })

  it('gives the strip a hit area at least 24 px tall, with full-height spells', () => {
    bar({ motionStrip: spells, onMotionSeek: vi.fn() })
    const strip = screen.getByTestId('motion-strip')
    expect(strip.className).toMatch(/(^|\s)h-(6|7|8|9|10|11|12)(\s|$)/)
    for (const b of within(strip).getAllByRole('button')) expect(b.className).toMatch(/(^|\s)h-full(\s|$)/)
  })

  describe('on a phone, with a day of 24 narrow spells', () => {
    // 24 spells on a 350 px strip are about 14 px wide, with a sliver of gap between them.
    const day: MotionSpellTick[] = Array.from({ length: 24 }, (_, i) => ({
      id: `p:${i}`, from: i / 24 + 0.003, to: (i + 1) / 24 - 0.003, clock: i * 100, band: 'lively', label: `Lively spell ${i}`,
    }))
    function strip(onMotionSeek: (clock: number) => void) {
      bar({ motionStrip: day, onMotionSeek })
      const el = screen.getByTestId('motion-strip')
      el.getBoundingClientRect = () => ({ left: 20, width: 350, right: 370, top: 0, bottom: 24, height: 24, x: 20, y: 0, toJSON: () => ({}) })
      return el
    }
    const tap = (el: HTMLElement, fraction: number) => fireEvent.pointerUp(el, {
      clientX: 20 + 350 * fraction, clientY: 12, pointerId: 1, isPrimary: true, pointerType: 'touch', button: 0, bubbles: true,
    })

    it('seeks to the nearest spell when a finger lands in the sliver between two', () => {
      const onMotionSeek = vi.fn()
      const el = strip(onMotionSeek)
      tap(el, 6 / 24 + 0.0005) // in the gap, a hair closer to spell 6 than to spell 5
      expect(onMotionSeek.mock.calls).toEqual([[600]])
    })

    it('seeks to the first or last spell when the tap lands past either end', () => {
      const onMotionSeek = vi.fn()
      const el = strip(onMotionSeek)
      tap(el, 0.0005)
      tap(el, 0.9999)
      expect(onMotionSeek.mock.calls).toEqual([[0], [2300]])
    })

    it('does not seek twice when the finger lands on a spell itself', () => {
      const onMotionSeek = vi.fn()
      strip(onMotionSeek)
      const spell = screen.getByRole('button', { name: 'Lively spell 9' })
      fireEvent.pointerUp(spell, { clientX: 20 + 350 * (9.5 / 24), pointerType: 'touch', bubbles: true })
      fireEvent.click(spell)
      expect(onMotionSeek.mock.calls).toEqual([[900]])
    })
  })

  it('keeps the spells out of the tab order, since the scrubber already seeks', () => {
    bar({ motionStrip: spells, onMotionSeek: vi.fn() })
    const buttons = within(screen.getByTestId('motion-strip')).getAllByRole('button')
    expect(buttons).toHaveLength(4)
    for (const b of buttons) expect(b).toHaveAttribute('tabindex', '-1')
  })

  it('is disabled with the rest of the bar', () => {
    bar({ motionStrip: spells, disabled: true })
    for (const b of within(screen.getByTestId('motion-strip')).getAllByRole('button')) expect(b).toBeDisabled()
  })
})

describe('the passages list', () => {
  const stats = {
    distance_nm: 12.3, underway_seconds: 7500, mean_sog_kts: null, max_sog_kts: null, fuel_burned_l: null,
    tws_mean_kts: null, max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
  }
  function row(id: string, over: Partial<PassageSummary> = {}, felt_deg: number | null = null): PassageSummary {
    return {
      id, start: '2026-03-13T23:30:00Z', end: '2026-03-14T02:00:00Z', from_name: 'Airlie', to_name: 'Hamilton',
      title: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '2026-03-14T03:00:00Z',
      stats: { ...stats, felt_deg },
      felt_hint: felt_deg === null ? 'No motion was recorded on this passage.' : undefined,
      timezone: 'Australia/Brisbane', ...over,
    }
  }
  const props = {
    filter: { kind: 'all' } as const, cruises: [], loading: false, error: null, canWrite: true,
    onRetry: vi.fn(), onFilter: vi.fn(), onOpenPassage: vi.fn(), onOpenCruise: vi.fn(), onCombine: vi.fn(),
  }

  it('shows the band word, the figure until bands are learned, and a dash without motion', () => {
    window.matchMedia = window.matchMedia ?? (((q: string) => ({ matches: false, media: q, addEventListener: vi.fn(), removeEventListener: vi.fn() })) as never)
    render(<PassagesPanel {...props} passages={[
      row('a', { felt_band: 'lively' }, 2.4),
      row('b', { felt_band: null, start: '2026-03-10T00:00:00Z' }, 1.2),
      row('c', { start: '2026-03-08T00:00:00Z' }, null),
    ]} />)
    expect(screen.getAllByText('Lively').length).toBeGreaterThan(0)
    expect(screen.getAllByText('1.2°').length).toBeGreaterThan(0)
    expect(screen.getAllByTitle('No motion was recorded on this passage.').length).toBeGreaterThan(0)
  })
})
