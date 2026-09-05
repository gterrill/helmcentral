import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { WindCompass } from '@/components/wind-compass'

function baseProps() {
  return {
    headingTrue: 0,
    windAngleApparentDeg: 45,
    windSide: 'starboard' as const,
    windAngleRelativeDeg: 45,
    windSpeedKts: 16.3,
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
    render(<WindCompass headingTrue={null} windAngleApparentDeg={null} windSide={null} windAngleRelativeDeg={null} windSpeedKts={null} />)

    const summary = screen.getByTestId('wind-compass-summary')
    expect(summary).toHaveTextContent(/unknown/)
  })
})
