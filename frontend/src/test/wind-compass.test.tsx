import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { WindCompass } from '@/components/wind-compass'

/** Parses the `rotate(Ndeg)` transform off a `<g>`'s inline style. */
function readRotationDeg(group: Element | null): number {
  const transform = (group as HTMLElement | null)?.style.transform ?? ''
  const match = /rotate\(([-\d.]+)deg\)/.exec(transform)
  if (!match) throw new Error(`no rotate() transform found in "${transform}"`)
  return Number.parseFloat(match[1])
}

// Course Up defaults: ring fixed (rotation carried entirely by ringRotationDeg,
// here 0 to match the old default headingTrue=0), bow fixed at the top
// (bowRotationDeg 0), arrow at a bow-relative angle.
function baseProps() {
  return {
    ringRotationDeg: 0,
    bowRotationDeg: 0,
    arrowAngleDeg: 45,
    windSide: 'starboard' as const,
    windAngleRelativeDeg: 45,
    windSpeedKts: 16.3,
    kind: 'apparent' as const,
  }
}

describe('WindCompass', () => {
  // Finding: hardcoded hex (#dc2626, #1e3a5f, #3b82f6, #d97706, #92400e)
  // means the compass silently stops adapting across themes.
  it('carries no hardcoded hex colour anywhere in the rendered markup', () => {
    const { container } = render(<WindCompass {...baseProps()} />)
    expect(container.innerHTML).not.toMatch(/#[0-9a-fA-F]{3,8}/)
  })

  it('renders the emphasised N cardinal from the primary token', () => {
    render(<WindCompass {...baseProps()} />)
    expect(screen.getByText('N')).toHaveAttribute('fill', 'hsl(var(--primary))')
  })

  it('renders the other cardinals from the muted-foreground token', () => {
    render(<WindCompass {...baseProps()} />)
    expect(screen.getByText('E')).toHaveAttribute('fill', 'hsl(var(--muted-foreground))')
    expect(screen.getByText('S')).toHaveAttribute('fill', 'hsl(var(--muted-foreground))')
    expect(screen.getByText('W')).toHaveAttribute('fill', 'hsl(var(--muted-foreground))')
  })

  // The signature instrument's fastest-changing number was the one readout
  // rendered in a proportional sans with no tabular figures.
  it('renders the wind speed hero in the display font with tabular figures', () => {
    render(<WindCompass {...baseProps()} />)
    const speed = screen.getByTestId('wind-speed-text')
    expect(speed).toHaveAttribute('font-family', expect.stringContaining('var(--font-display)'))
    expect(speed).toHaveStyle({ fontVariantNumeric: 'tabular-nums' })
    expect(speed).toHaveTextContent('16')
  })

  it('renders the side/angle readout in the display font with tabular figures too', () => {
    render(<WindCompass {...baseProps()} />)
    const sideAngle = screen.getByTestId('wind-side-angle-text')
    expect(sideAngle).toHaveAttribute('font-family', expect.stringContaining('var(--font-display)'))
    expect(sideAngle).toHaveStyle({ fontVariantNumeric: 'tabular-nums' })
    expect(sideAngle).toHaveTextContent('S45°')
  })

  // Two words and no data (aria-label="Wind compass") was the finding — the
  // SVG is now decorative and a DOM mirror carries the real readings.
  it('exposes the actual readings to a screen reader via a hidden DOM mirror, not just the SVG', () => {
    render(<WindCompass {...baseProps()} />)

    const svg = document.querySelector('svg')
    expect(svg).toHaveAttribute('aria-hidden', 'true')

    const summary = screen.getByTestId('wind-compass-summary')
    expect(summary).toHaveTextContent(/16 knots/)
    expect(summary).toHaveTextContent(/45°/)
    expect(summary).toHaveTextContent(/starboard/)
  })

  it('states unknown values plainly in the DOM mirror rather than fabricating a reading', () => {
    render(<WindCompass ringRotationDeg={0} bowRotationDeg={null} arrowAngleDeg={null} windSide={null} windAngleRelativeDeg={null} windSpeedKts={null} kind="apparent" />)

    const summary = screen.getByTestId('wind-compass-summary')
    expect(summary).toHaveTextContent(/unknown/)
  })

  // ADR 0129 — the wind-mode toggle. The screen-reader summary names which
  // reading is being shown, since the SVG itself carries no "APPARENT"/
  // "TRUE" label of its own (that lives on the tile's title chip).
  it('names the reading as apparent wind in the DOM mirror for kind="apparent"', () => {
    render(<WindCompass {...baseProps()} kind="apparent" />)
    expect(screen.getByTestId('wind-compass-summary')).toHaveTextContent(/^Wind compass\. Apparent wind speed/)
  })

  it('names the reading as true wind in the DOM mirror for kind="true"', () => {
    render(<WindCompass {...baseProps()} kind="true" />)
    expect(screen.getByTestId('wind-compass-summary')).toHaveTextContent(/^Wind compass\. True wind speed/)
  })

  // ADR 0129 — North Up orientation. WindCompass itself stays "pure": it
  // draws whatever ringRotationDeg/bowRotationDeg/arrowAngleDeg it is given,
  // trusting WindTile to have worked out the mode logic already.
  it('rotates the bow marker to bowRotationDeg instead of leaving it fixed', () => {
    const { container } = render(<WindCompass {...baseProps()} bowRotationDeg={70} />)
    const bow = container.querySelector('polygon')
    const group = bow?.closest('g')
    expect(group).toHaveStyle({ transform: 'rotate(70deg)' })
  })

  it('hides the bow marker entirely when bowRotationDeg is null, rather than guessing 0', () => {
    const { container } = render(<WindCompass {...baseProps()} bowRotationDeg={null} />)
    expect(container.querySelector('polygon')).not.toBeInTheDocument()
  })

  it('hides the wind arrow when arrowAngleDeg is null, rather than guessing 0', () => {
    const { container } = render(<WindCompass {...baseProps()} arrowAngleDeg={null} />)
    // The arrow's <path> is the only path element in the SVG.
    expect(container.querySelector('path')).not.toBeInTheDocument()
  })

  // Code-review fix (2026-09-25): the shortest-path unwrap accumulated the
  // rendered rotation onto a ref (prev + delta) every step, so after enough
  // steps in the same direction prev can end up more than 540° ahead of the
  // next raw angle - at which point the old `((angleDeg - prev + 540) %
  // 360) - 180` formula's inner value goes negative, and JS's `%` returns a
  // negative remainder (a truncating remainder, not a true modulo), which
  // sends the computed delta miles outside [-180, 180] and spins the marker
  // a full turn backwards in one render. A true modulo
  // (`((((angleDeg - prev) % 360) + 540) % 360) - 180`) stays correct at
  // any accumulated magnitude. This sequence (0, 120, 240, 0, 120, 240, 0,
  // 10) climbs the accumulated rotation past 720° by design - the old
  // formula broke exactly at the step where prev first exceeded 600.
  it('never jumps more than 180° between renders, even once the accumulated rotation passes 720° - for both the arrow and the bow marker', () => {
    const sequence = [0, 120, 240, 0, 120, 240, 0, 10]

    const { container, rerender } = render(
      <WindCompass {...baseProps()} arrowAngleDeg={sequence[0]} bowRotationDeg={sequence[0]} />,
    )
    const arrowGroup = () => container.querySelector('svg[data-testid="wind-compass-svg"] path')?.closest('g') ?? null
    const bowGroup = () => container.querySelector('svg[data-testid="wind-compass-svg"] polygon')?.closest('g') ?? null

    let prevArrow = readRotationDeg(arrowGroup())
    let prevBow = readRotationDeg(bowGroup())

    for (let i = 1; i < sequence.length; i++) {
      rerender(<WindCompass {...baseProps()} arrowAngleDeg={sequence[i]} bowRotationDeg={sequence[i]} />)

      const arrow = readRotationDeg(arrowGroup())
      const bow = readRotationDeg(bowGroup())

      expect(Math.abs(arrow - prevArrow)).toBeLessThanOrEqual(180)
      expect(Math.abs(bow - prevBow)).toBeLessThanOrEqual(180)

      prevArrow = arrow
      prevBow = bow
    }
  })
})
