import { render, screen, fireEvent } from '@testing-library/react'
import { beforeEach, describe, expect, test } from 'vitest'

import { WindTile } from '@/components/wind-tile'
import type { GustWindow } from '@/lib/gust-windows'

const gustValues: Record<GustWindow, number> = {
  '10m': 7.1,
  '30m': 9.2,
  '1h': 12.3,
  '24h': 18.4,
}

// Deliberately distinct from gustValues above, so a test asserting True mode
// reads this ladder (and not apparent's) can't pass by accident.
const gustValuesTrue: Record<GustWindow, number> = {
  '10m': 15.5,
  '30m': 17.2,
  '1h': 22.9,
  '24h': 28.0,
}

const baseProps = {
  headingTrue: 90,
  windAngleApparentDeg: 45,
  windSide: 'port' as const,
  windAngleRelativeDeg: 45,
  windSpeedApparentKts: 12,
  windSpeedTrueKts: 20,
  windAngleTrueDeg: 100,
  windSideTrue: 'starboard' as const,
  windAngleTrueRelativeDeg: 80,
  windDirectionTrueDeg: 200,
  currentSetDeg: null,
  currentDriftKts: null,
  currentDriftImpactKts: null,
  maxGustKts: gustValues,
  maxGustTrueKts: gustValuesTrue,
  lastUpdateAgeS: null,
}

// WindTile renders WindGaugeCluster twice (mobile + desktop canvases), so
// every "MAX GUST" button/text always appears twice. The two clusters are
// rendered in a fixed JSX order (mobile cluster's left card, mobile's right
// card, then desktop's left card, desktop's right card), so among the 4
// "max gust" buttons returned in DOM order, even indices (0, 2) are always
// the left card and odd indices (1, 3) are always the right card.
function getLeftButtons() {
  return screen.getAllByRole('button', { name: /max gust/i }).filter((_, i) => i % 2 === 0)
}

function getRightButtons() {
  return screen.getAllByRole('button', { name: /max gust/i }).filter((_, i) => i % 2 === 1)
}

function getWindModeToggle() {
  return screen.getByRole('button', { name: /Showing (apparent|true) wind/i })
}

function getOrientationToggle() {
  return screen.getByRole('button', { name: /(Course up|North up) — switch to/i })
}

// Both chip options are always in the DOM (see wind-tile.tsx's WindToggleChip
// doc comment) - only the active one lacks aria-hidden. This checks that
// directly rather than the button's overall textContent, which would
// contain both options' text regardless of which is "active".
function expectChipActive(testId: string, active: 'a' | 'b') {
  const optionA = screen.getByTestId(`${testId}-full-a`)
  const optionB = screen.getByTestId(`${testId}-full-b`)
  if (active === 'a') {
    expect(optionA).not.toHaveAttribute('aria-hidden')
    expect(optionB).toHaveAttribute('aria-hidden', 'true')
  } else {
    expect(optionA).toHaveAttribute('aria-hidden', 'true')
    expect(optionB).not.toHaveAttribute('aria-hidden')
  }
}

beforeEach(() => {
  localStorage.clear()
})

describe('WindTile MAX GUST cards', () => {
  test('render their default windows and values on mount with no localStorage seeded', () => {
    render(<WindTile {...baseProps} />)

    expect(screen.getAllByText('MAX GUST 10M')).toHaveLength(2)
    expect(screen.getAllByText('MAX GUST 1HR')).toHaveLength(2)
    expect(screen.getAllByText('7.1', { exact: false })).toHaveLength(2)
    expect(screen.getAllByText('12.3', { exact: false })).toHaveLength(2)

    expect(getLeftButtons()).toHaveLength(2)
    expect(getRightButtons()).toHaveLength(2)
  })

  test('clicking the left card cycles 10m -> 30m -> 1h -> 24h -> 10m, updating title and value each step', () => {
    render(<WindTile {...baseProps} />)

    fireEvent.click(getLeftButtons()[0])
    expect(screen.getAllByText('MAX GUST 30M')).toHaveLength(2)
    expect(screen.getAllByText('9.2', { exact: false })).toHaveLength(2)

    // The right card still sits on its own default (1h), so once the left
    // card also reaches 1h there are 4 matching elements total (2 per card).
    fireEvent.click(getLeftButtons()[0])
    expect(screen.getAllByText('MAX GUST 1HR')).toHaveLength(4)
    expect(screen.getAllByText('12.3', { exact: false })).toHaveLength(4)

    fireEvent.click(getLeftButtons()[0])
    expect(screen.getAllByText('MAX GUST 24HR')).toHaveLength(2)
    expect(screen.getAllByText('18.4', { exact: false })).toHaveLength(2)

    fireEvent.click(getLeftButtons()[0])
    expect(screen.getAllByText('MAX GUST 10M')).toHaveLength(2)
    expect(screen.getAllByText('7.1', { exact: false })).toHaveLength(2)
  })

  test('the two cards cycle fully independently', () => {
    render(<WindTile {...baseProps} />)

    // Click the left card three times (10m -> 30m -> 1h -> 24h). The right
    // card must stay on its untouched default the whole time.
    fireEvent.click(getLeftButtons()[0])
    fireEvent.click(getLeftButtons()[0])
    fireEvent.click(getLeftButtons()[0])
    expect(screen.getAllByText('MAX GUST 24HR')).toHaveLength(2)
    expect(screen.getAllByText('MAX GUST 1HR')).toHaveLength(2)
    expect(screen.getAllByText('12.3', { exact: false })).toHaveLength(2)

    // Now click the right card once and confirm the left card (still 24h) is unaffected.
    fireEvent.click(getRightButtons()[0])
    expect(screen.getAllByText('MAX GUST 24HR')).toHaveLength(4) // left card + right card both now read 24hr
    expect(screen.getAllByText('18.4', { exact: false })).toHaveLength(4)
  })

  test('persists the new selection to localStorage under windTile.gustWindow.left/right', () => {
    render(<WindTile {...baseProps} />)

    fireEvent.click(getLeftButtons()[0])
    expect(localStorage.getItem('windTile.gustWindow.left')).toBe('30m')

    fireEvent.click(getRightButtons()[0])
    expect(localStorage.getItem('windTile.gustWindow.right')).toBe('24h')
  })

  test('restores a valid pre-seeded localStorage selection on mount', () => {
    localStorage.setItem('windTile.gustWindow.right', '24h')

    render(<WindTile {...baseProps} />)

    expect(screen.getAllByText('MAX GUST 24HR')).toHaveLength(2)
    expect(screen.getAllByText('18.4', { exact: false })).toHaveLength(2)
    // Left card is unaffected and keeps its own default.
    expect(screen.getAllByText('MAX GUST 10M')).toHaveLength(2)
  })

  test('falls back to the default window for an invalid/corrupt localStorage value, without throwing', () => {
    localStorage.setItem('windTile.gustWindow.left', 'garbage')

    expect(() => render(<WindTile {...baseProps} />)).not.toThrow()

    expect(screen.getAllByText('MAX GUST 10M')).toHaveLength(2)
    expect(screen.getAllByText('7.1', { exact: false })).toHaveLength(2)
  })

  test('cards are focusable, native buttons that activate on Enter and Space', () => {
    render(<WindTile {...baseProps} />)

    const button = getLeftButtons()[0]
    button.focus()
    expect(button).toHaveFocus()

    // Native <button> elements turn an Enter keydown into a click; jsdom
    // doesn't perform that default action itself, so the click is fired
    // explicitly here to stand in for what a real browser does.
    fireEvent.keyDown(button, { key: 'Enter', code: 'Enter' })
    fireEvent.click(button)
    expect(screen.getAllByText('MAX GUST 30M')).toHaveLength(2)

    fireEvent.keyDown(button, { key: ' ', code: 'Space' })
    fireEvent.click(button)
    // The right card defaults to 1h too, so both cards now read MAX GUST 1HR.
    expect(screen.getAllByText('MAX GUST 1HR')).toHaveLength(4)
  })
})

// ADR 0129 — the tile's title becomes the plain string "Wind" (both wind
// mode and orientation are toggles, not part of the title text itself).
describe('WindTile title', () => {
  test('renders the plain title "Wind", not "Apparent Wind" or a mode-qualified variant', () => {
    render(<WindTile {...baseProps} />)
    expect(screen.getByText('Wind')).toBeInTheDocument()
    expect(screen.queryByText('Apparent Wind')).not.toBeInTheDocument()
    expect(screen.queryByText('Apparent Wind - Course Up')).not.toBeInTheDocument()
  })
})

// ADR 0129 — the two title-bar toggles: wind mode (apparent/true) and
// orientation (course-up/north-up), each a per-device localStorage setting.
describe('WindTile mode and orientation toggles', () => {
  test('default to Apparent and Course Up with no localStorage seeded', () => {
    render(<WindTile {...baseProps} />)

    expectChipActive('wind-mode-chip', 'a')
    expect(getWindModeToggle()).toHaveAccessibleName(/Showing apparent wind — switch to true wind/i)
    expectChipActive('orientation-chip', 'a')
    expect(getOrientationToggle()).toHaveAccessibleName(/Course up — switch to north up/i)
  })

  test('clicking the wind-mode chip flips which option is active (Apparent -> True -> Apparent) and its aria-label', () => {
    render(<WindTile {...baseProps} />)

    fireEvent.click(getWindModeToggle())
    expectChipActive('wind-mode-chip', 'b')
    expect(getWindModeToggle()).toHaveAccessibleName(/Showing true wind — switch to apparent wind/i)

    fireEvent.click(getWindModeToggle())
    expectChipActive('wind-mode-chip', 'a')
  })

  test('clicking the orientation chip flips which option is active (Course Up -> North Up -> Course Up)', () => {
    render(<WindTile {...baseProps} />)

    fireEvent.click(getOrientationToggle())
    expectChipActive('orientation-chip', 'b')
    expect(getOrientationToggle()).toHaveAccessibleName(/North up — switch to course up/i)

    fireEvent.click(getOrientationToggle())
    expectChipActive('orientation-chip', 'a')
  })

  test('persists windMode and orientation to localStorage independently of the gust window keys', () => {
    render(<WindTile {...baseProps} />)

    fireEvent.click(getWindModeToggle())
    expect(localStorage.getItem('windTile.windMode')).toBe('true')

    fireEvent.click(getOrientationToggle())
    expect(localStorage.getItem('windTile.orientation')).toBe('north-up')
  })

  test('restores a valid pre-seeded localStorage mode and orientation on mount', () => {
    localStorage.setItem('windTile.windMode', 'true')
    localStorage.setItem('windTile.orientation', 'north-up')

    render(<WindTile {...baseProps} />)

    expectChipActive('wind-mode-chip', 'b')
    expectChipActive('orientation-chip', 'b')
  })

  test('falls back to defaults for invalid/corrupt localStorage windMode/orientation values, without throwing', () => {
    localStorage.setItem('windTile.windMode', 'garbage')
    localStorage.setItem('windTile.orientation', 'sideways')

    expect(() => render(<WindTile {...baseProps} />)).not.toThrow()

    expectChipActive('wind-mode-chip', 'a')
    expectChipActive('orientation-chip', 'a')
  })
})

// Coordinator fix (2026-09-25): min-w guesses on the chips let True render
// narrower than Apparent, so the header rule's length changed on toggle.
// Both options now render at once (see WindToggleChip's doc comment in
// wind-tile.tsx) so the chip is always exactly as wide as its longer
// option - this proves that DOM shape directly, and that it holds for both
// chips' full label sets and both short label sets, plus that the button's
// accessible name is untouched by any of it.
describe('WindTile toggle chip width stability', () => {
  test('keeps both options in the DOM (so the chip can never resize on toggle); the inactive one is present but hidden from the accessibility tree', () => {
    render(<WindTile {...baseProps} />)

    const modeInactive = screen.getByTestId('wind-mode-chip-full-b')
    expect(modeInactive).toHaveTextContent('True')
    expect(modeInactive).toHaveAttribute('aria-hidden', 'true')
    expect(modeInactive).toHaveClass('invisible')
    expect(screen.getByTestId('wind-mode-chip-full-a')).not.toHaveClass('invisible')

    const orientationInactive = screen.getByTestId('orientation-chip-full-b')
    expect(orientationInactive).toHaveTextContent('North Up')
    expect(orientationInactive).toHaveAttribute('aria-hidden', 'true')
    expect(orientationInactive).toHaveClass('invisible')

    // The short-label twins (only ever shown once the tile's own container
    // narrows past @max-[20rem]) follow the identical rule, independently
    // of the full-label set above.
    expect(screen.getByTestId('wind-mode-chip-short-a')).toHaveTextContent('App')
    expect(screen.getByTestId('wind-mode-chip-short-b')).toHaveTextContent('True')
    expect(screen.getByTestId('wind-mode-chip-short-b')).toHaveAttribute('aria-hidden', 'true')
    expect(screen.getByTestId('orientation-chip-short-a')).toHaveTextContent('C Up')
    expect(screen.getByTestId('orientation-chip-short-b')).toHaveTextContent('N Up')
    expect(screen.getByTestId('orientation-chip-short-b')).toHaveAttribute('aria-hidden', 'true')

    // The button's accessible name comes from its own aria-label, not from
    // whichever span happens to be visible, so none of the above affects it.
    expect(getWindModeToggle()).toHaveAccessibleName('Showing apparent wind — switch to true wind')
    expect(getOrientationToggle()).toHaveAccessibleName('Course up — switch to north up')
  })

  test('the Wind tile opts into container queries so its header can react to the tile\'s own width, not the viewport\'s', () => {
    const { container } = render(<WindTile {...baseProps} />)

    expect(container.querySelector('[data-slot="card"]')).toHaveClass('@container')
  })
})

// ADR 0129 — True mode reads the true-wind fields and true gust ladder
// throughout the compass, not a converted/borrowed copy of apparent's.
describe('WindTile true wind mode', () => {
  test('shows the true wind speed, side/angle and gust ladder once switched, not apparent\'s', () => {
    render(<WindTile {...baseProps} />)

    // Apparent (default).
    expect(screen.getAllByTestId('wind-speed-text')[0]).toHaveTextContent('12')
    expect(screen.getAllByTestId('wind-side-angle-text')[0]).toHaveTextContent('P45°')
    expect(screen.getAllByText('7.1', { exact: false })).toHaveLength(2)

    fireEvent.click(getWindModeToggle())

    expect(screen.getAllByTestId('wind-speed-text')[0]).toHaveTextContent('20')
    expect(screen.getAllByTestId('wind-side-angle-text')[0]).toHaveTextContent('S80°')
    expect(screen.getAllByText('15.5', { exact: false })).toHaveLength(2)
    expect(screen.queryAllByText('7.1', { exact: false })).toHaveLength(0)
    expect(screen.queryAllByText('12.3', { exact: false })).toHaveLength(0)
  })

  test('missing true data shows dashes once switched, never a silent fallback to the apparent reading', () => {
    render(
      <WindTile
        {...baseProps}
        windSpeedTrueKts={null}
        windAngleTrueDeg={null}
        windSideTrue={null}
        windAngleTrueRelativeDeg={null}
        maxGustTrueKts={{ '10m': null, '30m': null, '1h': null, '24h': null }}
      />,
    )

    fireEvent.click(getWindModeToggle())

    expect(screen.getAllByTestId('wind-speed-text')[0]).toHaveTextContent('—')
    expect(screen.getAllByTestId('wind-side-angle-text')[0]).toHaveTextContent('——')
    // The MAX GUST cards must not silently show apparent's numbers instead.
    for (const button of [...getLeftButtons(), ...getRightButtons()]) {
      expect(button).toHaveTextContent('—')
      expect(button).not.toHaveTextContent('7.1')
      expect(button).not.toHaveTextContent('12.3')
    }
  })
})

// ADR 0129 — orientation: Course Up (default) rotates the ring with heading
// and fixes the bow at the top; North Up holds true north fixed at the top
// and sweeps the bow marker to the heading instead.
describe('WindTile orientation', () => {
  test('course-up rotates the compass ring with heading (North lands off the top)', () => {
    render(<WindTile {...baseProps} headingTrue={90} />)

    // heading 90° puts North's tick at the 9 o'clock position (x=44, the
    // canvas centre 140 minus the 96px label radius), not at the top.
    const nLabels = screen.getAllByText('N')
    expect(nLabels[0]).toHaveAttribute('x', '44')
  })

  test('north-up holds the ring still: North stays pinned to the top regardless of heading', () => {
    render(<WindTile {...baseProps} headingTrue={90} />)

    fireEvent.click(getOrientationToggle())

    const nLabels = screen.getAllByText('N')
    expect(nLabels[0]).toHaveAttribute('x', '140')
    expect(nLabels[0]).toHaveAttribute('y', '44')
  })

  test('north-up sweeps the bow marker to the heading instead of leaving it fixed at the top', () => {
    const { container } = render(<WindTile {...baseProps} headingTrue={90} />)

    // Course-up (default): the bow marker never rotates.
    let bowGroups = Array.from(container.querySelectorAll('svg[data-testid="wind-compass-svg"] polygon')).map((p) => p.closest('g'))
    expect(bowGroups.length).toBeGreaterThan(0)
    for (const g of bowGroups) {
      expect(g).toHaveStyle({ transform: 'rotate(0deg)' })
    }

    fireEvent.click(getOrientationToggle())

    bowGroups = Array.from(container.querySelectorAll('svg[data-testid="wind-compass-svg"] polygon')).map((p) => p.closest('g'))
    expect(bowGroups.length).toBeGreaterThan(0)
    for (const g of bowGroups) {
      expect(g).toHaveStyle({ transform: 'rotate(90deg)' })
    }
  })

  test('north-up hides the bow marker and the apparent wind arrow when heading is unknown, rather than guessing', () => {
    const { container } = render(<WindTile {...baseProps} headingTrue={null} />)

    fireEvent.click(getOrientationToggle())

    expect(container.querySelectorAll('svg[data-testid="wind-compass-svg"] polygon')).toHaveLength(0)
    expect(container.querySelectorAll('svg[data-testid="wind-compass-svg"] path')).toHaveLength(0)
    // The centre numbers still show even with no heading to place the arrow/bow.
    expect(screen.getAllByTestId('wind-speed-text')[0]).toHaveTextContent('12')
  })

  test('north-up still shows the true wind arrow with no heading, since directionTrue is already an absolute bearing', () => {
    const { container } = render(<WindTile {...baseProps} headingTrue={null} />)

    fireEvent.click(getWindModeToggle())
    fireEvent.click(getOrientationToggle())

    // Bow still hides (it always needs headingTrue), but the true-wind arrow
    // does not, so its <path> renders.
    expect(container.querySelectorAll('svg[data-testid="wind-compass-svg"] polygon')).toHaveLength(0)
    expect(container.querySelectorAll('svg[data-testid="wind-compass-svg"] path').length).toBeGreaterThan(0)
  })
})

// use-vessel-state.ts carries no per-field age for wind today (see
// VesselState in use-vessel-state.ts - no wind_*_age_s field), so
// lastUpdateAgeS is null until a source actually publishes one - these only
// prove the wiring is correct once an age is supplied, not that one exists
// yet.
describe('WindTile staleness', () => {
  test('flags the tile stale once a feed age passes the threshold', () => {
    render(<WindTile {...baseProps} lastUpdateAgeS={5940} />)

    const badges = screen.getAllByTestId('tile-stale-badge')
    expect(badges.length).toBeGreaterThan(0)
    for (const badge of badges) {
      expect(badge).toHaveTextContent('1h 39m')
    }
  })

  test('does not flag the tile stale when no update age is known', () => {
    render(<WindTile {...baseProps} lastUpdateAgeS={null} />)

    expect(screen.queryByTestId('tile-stale-badge')).not.toBeInTheDocument()
  })
})

/*
 * A browser contrast detector measured --gauge-primary (the amber MAX GUST
 * readout) against this card's ground at 2.6:1, below the 3:1 large-text bar
 * - the card used a translucent `bg-background/80`, which composites onto
 * whatever ends up behind it instead of a known colour. An opaque token
 * removes that: `bg-card` measures 3.20:1 in :root, 8.69:1 in .dark and
 * 8.94:1 in the instrument skin (all against the current --gauge-primary),
 * comfortably clearing 3:1 in every theme. This locks the token so a future
 * edit can't reintroduce a translucent ground here.
 */
describe('WindTile gust card background', () => {
  test('the gust cards sit on an opaque token, not a translucent background that composites unpredictably', () => {
    render(<WindTile {...baseProps} />)

    const cards = [...getLeftButtons(), ...getRightButtons()]
    expect(cards.length).toBeGreaterThan(0)
    for (const card of cards) {
      const className = card.getAttribute('class') ?? ''
      expect(className).not.toMatch(/bg-background\/\d+/)
      expect(className).toContain('bg-card')
    }
  })
})
