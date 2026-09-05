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
    // state word read as unknown, not as a frozen "CRIT" claim.
    expect(screen.getAllByText('—')).toHaveLength(2)
    expect(screen.queryByText('CRIT')).not.toBeInTheDocument()
  })

  it('does not go stale when no age has ever been reported for this source', () => {
    render(<TanksTile tanks={[tank({})]} loading={false} lastUpdateAgeS={null} />)

    expect(screen.queryByTestId('tile-stale-badge')).not.toBeInTheDocument()
  })
})
