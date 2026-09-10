import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'

import { BulletGauge } from '@/components/ui/bullet-gauge'

describe('BulletGauge', () => {
  it('clamps the value bar width to 100 when value is above max', () => {
    render(<BulletGauge value={150} min={0} max={100} bandLow={null} bandHigh={null} unit="kt" />)
    const bar = screen.getByTestId('bullet-gauge-value')
    expect(Number(bar.getAttribute('width'))).toBe(100)
  })

  it('clamps the value bar width to 0 when value is below min', () => {
    render(<BulletGauge value={-50} min={0} max={100} bandLow={null} bandHigh={null} unit="kt" />)
    const bar = screen.getByTestId('bullet-gauge-value')
    expect(Number(bar.getAttribute('width'))).toBe(0)
  })

  it('draws the value bar at the correct fraction for an in-range value', () => {
    render(<BulletGauge value={25} min={0} max={100} bandLow={null} bandHigh={null} unit="kt" />)
    const bar = screen.getByTestId('bullet-gauge-value')
    expect(Number(bar.getAttribute('width'))).toBe(25)
  })

  it('renders the band at the correct x/width when both bounds are given', () => {
    render(<BulletGauge value={null} min={0} max={100} bandLow={20} bandHigh={60} unit="kt" />)
    const band = screen.getByTestId('bullet-gauge-band')
    expect(Number(band.getAttribute('x'))).toBe(20)
    expect(Number(band.getAttribute('width'))).toBe(40)
  })

  it('omits the band when bandLow is null', () => {
    render(<BulletGauge value={null} min={0} max={100} bandLow={null} bandHigh={60} unit="kt" />)
    expect(screen.queryByTestId('bullet-gauge-band')).not.toBeInTheDocument()
  })

  it('omits the band when bandHigh is null', () => {
    render(<BulletGauge value={null} min={0} max={100} bandLow={20} bandHigh={null} unit="kt" />)
    expect(screen.queryByTestId('bullet-gauge-band')).not.toBeInTheDocument()
  })

  it('renders up to two markers with correct aria-labels, and only the second is dashed', () => {
    render(
      <BulletGauge
        value={50}
        min={0}
        max={100}
        bandLow={null}
        bandHigh={null}
        unit="kt"
        markers={[
          { value: 30, label: 'Avg' },
          { value: 70, label: 'Max' },
          { value: 90, label: 'Ignored' },
        ]}
      />,
    )
    const first = screen.getByLabelText('Avg: 30 kt')
    const second = screen.getByLabelText('Max: 70 kt')
    expect(screen.queryByLabelText('Ignored: 90 kt')).not.toBeInTheDocument()

    const firstTick = first.matches('line, rect') ? first : first.querySelector('line, rect')
    const secondTick = second.matches('line, rect') ? second : second.querySelector('line, rect')
    expect(firstTick).not.toBeNull()
    expect(secondTick).not.toBeNull()
    expect(firstTick!.getAttribute('stroke-dasharray')).toBeNull()
    expect(secondTick!.getAttribute('stroke-dasharray')).not.toBeNull()
  })

  it('renders the track (and band/markers) but no value-bar element when value is null', () => {
    render(
      <BulletGauge
        value={null}
        min={0}
        max={100}
        bandLow={20}
        bandHigh={60}
        unit="kt"
        markers={[{ value: 30, label: 'Avg' }]}
      />,
    )
    expect(screen.queryByTestId('bullet-gauge-value')).not.toBeInTheDocument()
    expect(screen.getByTestId('bullet-gauge-track')).toBeInTheDocument()
    expect(screen.getByTestId('bullet-gauge-band')).toBeInTheDocument()
    expect(screen.getByLabelText('Avg: 30 kt')).toBeInTheDocument()
  })

  it('renders min/max labels with the unit suffix', () => {
    render(<BulletGauge value={50} min={5} max={95} bandLow={null} bandHigh={null} unit="kt" />)
    expect(screen.getByText('5kt')).toBeInTheDocument()
    expect(screen.getByText('95kt')).toBeInTheDocument()
  })
})
