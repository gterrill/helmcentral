import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { Tile } from '@/components/ui/tile'
import { severityBorderClass, type ZoneState } from '@/lib/severity'

/**
 * The tile edge carries the worst zone state of its readings (ADR 0081).
 */
describe('Tile state', () => {
  test.each<ZoneState>(['alert', 'warn', 'outside', 'alarm', 'emergency'])(
    'carries the %s state as a border class, data-state and a header dot',
    (state) => {
      const { container } = render(
        <Tile title="Engine" state={state}>
          <p>content</p>
        </Tile>,
      )
      const card = container.querySelector('[data-slot="card"]')!
      expect(card).toHaveAttribute('data-state', state)
      expect(card).toHaveClass(...severityBorderClass(state).split(' '))
      expect(screen.getByTestId('tile-state-dot')).toBeInTheDocument()
      expect(screen.getByTestId('tile-state-dot')).toHaveAttribute('aria-label', `State: ${state}`)
    },
  )

  test('normal renders no border class, no data-state and no dot', () => {
    const { container } = render(
      <Tile title="Engine" state="normal">
        <p>content</p>
      </Tile>,
    )
    const card = container.querySelector('[data-slot="card"]')!
    expect(card).not.toHaveAttribute('data-state')
    expect(screen.queryByTestId('tile-state-dot')).not.toBeInTheDocument()
  })

  test('no state prop renders neither', () => {
    const { container } = render(
      <Tile title="Engine">
        <p>content</p>
      </Tile>,
    )
    const card = container.querySelector('[data-slot="card"]')!
    expect(card).not.toHaveAttribute('data-state')
    expect(screen.queryByTestId('tile-state-dot')).not.toBeInTheDocument()
  })

  test('an explicit null state renders neither', () => {
    const { container } = render(
      <Tile title="Engine" state={null}>
        <p>content</p>
      </Tile>,
    )
    const card = container.querySelector('[data-slot="card"]')!
    expect(card).not.toHaveAttribute('data-state')
    expect(screen.queryByTestId('tile-state-dot')).not.toBeInTheDocument()
  })

  // Stale wins: a value the tile can no longer vouch for should not also
  // claim a state (see AGENTS.md's fallback policy — no invented state).
  test('a stale tile suppresses the state border, dot and data-state even with an alarm state', () => {
    const { container } = render(
      <Tile title="Engine" state="alarm" stale staleLabel="1h 39m">
        <p>content</p>
      </Tile>,
    )
    const card = container.querySelector('[data-slot="card"]')!
    expect(card).not.toHaveAttribute('data-state')
    expect(card).not.toHaveClass('border-red-500')
    expect(card).toHaveClass('border-amber-500')
    expect(screen.queryByTestId('tile-state-dot')).not.toBeInTheDocument()
  })

  test('everything else about the tile stays as it was', () => {
    render(
      <Tile title="Engine" state="warn">
        <p>content</p>
      </Tile>,
    )
    expect(screen.getByRole('heading', { level: 2, name: 'Engine' })).toBeInTheDocument()
  })
})
