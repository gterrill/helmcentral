import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { TanksTile } from '@/components/tanks-tile'
import type { TankLevel } from '@/hooks/use-tanks-state'

function tank(overrides: Partial<TankLevel>): TankLevel {
  return {
    id: 'tank-1',
    label: 'Fuel',
    category: 'fuel',
    kind: 'fuel',
    level_percent: 60,
    ...overrides,
  }
}

describe('TanksTile', () => {
  // Finding: eight tank rows sat permanently green on a live board. A
  // healthy tank must render with no alert colour at all — neither on the
  // label, the bar nor the number — and the earlier hardcoded hex is gone
  // entirely.
  it('renders a healthy tank with no alert colour on the label, bar or number', () => {
    render(<TanksTile tanks={[tank({ kind: 'fuel', level_percent: 60 })]} loading={false} lastUpdateAgeS={null} />)

    const label = screen.getByText('Fuel')
    expect(label).not.toHaveClass('text-amber-600', 'text-red-600')
    expect(label).toHaveClass('text-foreground')

    const percent = screen.getByText('60%')
    expect(percent).toHaveClass('text-foreground')
    expect(percent).not.toHaveClass('text-amber-600', 'text-red-600')

    expect(screen.getByText('OK')).toBeInTheDocument()

    const { container } = render(<TanksTile tanks={[tank({ kind: 'fuel', level_percent: 60 })]} loading={false} lastUpdateAgeS={null} />)
    expect(container.innerHTML).not.toMatch(/#[0-9a-fA-F]{3,8}/)
  })

  it('warns a low fuel tank in amber with a LOW word, not the resting-normal colour', () => {
    render(<TanksTile tanks={[tank({ kind: 'fuel', level_percent: 20 })]} loading={false} lastUpdateAgeS={null} />)

    expect(screen.getByText('20%')).toHaveClass('text-amber-600')
    expect(screen.getByText('LOW')).toBeInTheDocument()
  })

  it('flags a critically low fuel tank in red with a CRIT word', () => {
    render(<TanksTile tanks={[tank({ kind: 'fuel', level_percent: 5 })]} loading={false} lastUpdateAgeS={null} />)

    expect(screen.getByText('5%')).toHaveClass('text-red-600')
    expect(screen.getByText('CRIT')).toBeInTheDocument()
  })

  // Water inverts: a nearly-full water tank is healthy, a nearly-empty one
  // is critical. Preserve that direction exactly.
  it('treats a full water tank as healthy and a near-empty one as critical', () => {
    render(
      <TanksTile
        tanks={[
          tank({ id: 'w1', label: 'Fresh Water', kind: 'water', level_percent: 95 }),
          tank({ id: 'w2', label: 'Fresh Water 2', kind: 'water', level_percent: 8 }),
        ]}
        loading={false}
        lastUpdateAgeS={null}
      />,
    )

    expect(screen.getByText('95%')).toHaveClass('text-foreground')
    expect(screen.getByText('8%')).toHaveClass('text-red-600')
  })

  // Waste inverts again: a nearly-full waste tank is the bad state, so its
  // warn/critical word reads HIGH, not LOW.
  it('flags a nearly-full waste tank as warn with a HIGH word, and overflowing as critical', () => {
    render(
      <TanksTile
        tanks={[
          tank({ id: 'ws1', label: 'Waste', kind: 'waste', level_percent: 85 }),
          tank({ id: 'ws2', label: 'Waste 2', kind: 'waste', level_percent: 95 }),
        ]}
        loading={false}
        lastUpdateAgeS={null}
      />,
    )

    expect(screen.getByText('85%')).toHaveClass('text-amber-600')
    expect(screen.getByText('HIGH')).toBeInTheDocument()
    expect(screen.getByText('95%')).toHaveClass('text-red-600')
    expect(screen.getByText('CRIT')).toBeInTheDocument()
  })

  it('greys the tile and blanks readings once the feed is stale', () => {
    render(<TanksTile tanks={[tank({ level_percent: 5 })]} loading={false} lastUpdateAgeS={999} />)

    expect(screen.getByTestId('tile-stale-badge')).toBeInTheDocument()
    // A stale critical reading can't be trusted, so both the percent and its
    // state word read as unknown, not as a frozen "CRIT" claim. Three more
    // dashes come from the fuel footer (ADR 0084), which renders here too —
    // the default tank() fixture is a fuel tank, and no fuel-derived props
    // were supplied, so all three of its figures are absent as well.
    expect(screen.getAllByText('—')).toHaveLength(5)
    expect(screen.queryByText('CRIT')).not.toBeInTheDocument()
  })

  it('does not go stale when no age has ever been reported for this source', () => {
    render(<TanksTile tanks={[tank({})]} loading={false} lastUpdateAgeS={null} />)

    expect(screen.queryByTestId('tile-stale-badge')).not.toBeInTheDocument()
  })

  // The tile edge carries the worst tank tone (ADR 0081).
  describe('tile state (ADR 0081)', () => {
    it('carries a critical tank as the alarm state', () => {
      const { container } = render(
        <TanksTile
          tanks={[tank({ id: 't1', kind: 'fuel', level_percent: 60 }), tank({ id: 't2', kind: 'fuel', level_percent: 5 })]}
          loading={false} lastUpdateAgeS={null}
        />,
      )
      expect(container.querySelector('[data-slot="card"]')).toHaveAttribute('data-state', 'alarm')
    })

    it('carries a warn tank as the warn state when nothing is worse', () => {
      const { container } = render(
        <TanksTile tanks={[tank({ kind: 'fuel', level_percent: 20 })]} loading={false} lastUpdateAgeS={null} />,
      )
      expect(container.querySelector('[data-slot="card"]')).toHaveAttribute('data-state', 'warn')
    })

    it('carries no state when every tank is healthy', () => {
      const { container } = render(
        <TanksTile tanks={[tank({ kind: 'fuel', level_percent: 60 })]} loading={false} lastUpdateAgeS={null} />,
      )
      expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-state')
    })

    it('suppresses the state on a stale feed', () => {
      const { container } = render(
        <TanksTile tanks={[tank({ kind: 'fuel', level_percent: 5 })]} loading={false} lastUpdateAgeS={999} />,
      )
      expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-state')
    })
  })

  // The footer under the tank bars (ADR 0084): fuel aboard, range and time
  // to empty at the current instantaneous burn.
  describe('fuel footer (ADR 0084)', () => {
    it('renders all three figures from a mocked payload when a fuel tank is listed', () => {
      render(
        <TanksTile
          tanks={[tank({ kind: 'fuel' })]}
          loading={false}
          lastUpdateAgeS={null}
          fuelVolumeM3={3.0987}
          fuelVolumeAgeS={17}
          fuelTimeToEmptyS={3718500}
          fuelRangeM={225700}
          fuelDerivedAgeS={17}
        />,
      )

      expect(screen.getByText('Fuel aboard')).toBeInTheDocument()
      // 3.0987 m3 -> 3099 L, no decimals.
      expect(screen.getByText('3099')).toBeInTheDocument()

      expect(screen.getByText('Range at current burn')).toBeInTheDocument()
      // 225700 m -> 121.9 nm.
      expect(screen.getByText('121.9')).toBeInTheDocument()

      expect(screen.getByText('Time to empty')).toBeInTheDocument()
      // 3718500 s -> 1032.9 h, one decimal.
      expect(screen.getByText('1032.9')).toBeInTheDocument()

      expect(screen.queryByTestId('fuel-footer-stale-badge')).not.toBeInTheDocument()
    })

    it('renders the structural dash for each figure the backend reports absent', () => {
      render(
        <TanksTile
          tanks={[tank({ kind: 'fuel' })]}
          loading={false}
          lastUpdateAgeS={null}
          fuelVolumeM3={null}
          fuelVolumeAgeS={null}
          fuelTimeToEmptyS={null}
          fuelRangeM={null}
          fuelDerivedAgeS={null}
        />,
      )

      // One dash per footer stat, on top of whatever the tank rows render.
      expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(3)
    })

    it('blanks fuel aboard with its own stale badge, independent of range and time', () => {
      render(
        <TanksTile
          tanks={[tank({ kind: 'fuel' })]}
          loading={false}
          lastUpdateAgeS={null}
          fuelVolumeM3={3.0987}
          fuelVolumeAgeS={90000}
          fuelTimeToEmptyS={3718500}
          fuelRangeM={225700}
          fuelDerivedAgeS={17}
        />,
      )

      const fuelAboardLabel = screen.getByText('Fuel aboard')
      expect(fuelAboardLabel.parentElement).toHaveTextContent('Stale')
      expect(screen.queryByText('3099')).not.toBeInTheDocument()

      // Range and time are keyed off fuel_derived_age_s, which is fresh here.
      expect(screen.getByText('121.9')).toBeInTheDocument()
      expect(screen.getByText('1032.9')).toBeInTheDocument()
    })

    it('blanks range and time with a stale badge when fuel_derived_age_s is stale, independent of fuel aboard', () => {
      render(
        <TanksTile
          tanks={[tank({ kind: 'fuel' })]}
          loading={false}
          lastUpdateAgeS={null}
          fuelVolumeM3={3.0987}
          fuelVolumeAgeS={17}
          fuelTimeToEmptyS={3718500}
          fuelRangeM={225700}
          fuelDerivedAgeS={73570}
        />,
      )

      // Fuel aboard is unaffected: it goes stale on its own age only.
      expect(screen.getByText('3099')).toBeInTheDocument()

      const rangeLabel = screen.getByText('Range at current burn')
      expect(rangeLabel.parentElement).toHaveTextContent('Stale')
      const timeLabel = screen.getByText('Time to empty')
      expect(timeLabel.parentElement).toHaveTextContent('Stale')

      expect(screen.queryByText('121.9')).not.toBeInTheDocument()
      expect(screen.queryByText('1032.9')).not.toBeInTheDocument()
    })

    it('omits the footer entirely when no fuel tank is listed', () => {
      render(
        <TanksTile
          tanks={[tank({ id: 'w1', label: 'Fresh Water', kind: 'water', level_percent: 80 })]}
          loading={false}
          lastUpdateAgeS={null}
          fuelVolumeM3={3.0987}
          fuelVolumeAgeS={17}
          fuelTimeToEmptyS={3718500}
          fuelRangeM={225700}
          fuelDerivedAgeS={17}
        />,
      )

      expect(screen.queryByText('Fuel aboard')).not.toBeInTheDocument()
      expect(screen.queryByText('Range at current burn')).not.toBeInTheDocument()
      expect(screen.queryByText('Time to empty')).not.toBeInTheDocument()
    })
  })
})
