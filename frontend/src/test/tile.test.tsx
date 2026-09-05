import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { Tile } from '@/components/ui/tile'

test('renders the tile title as an h2 so a screen reader can jump between tiles', () => {
  render(
    <Tile title="Battery & Power">
      <p>content</p>
    </Tile>,
  )

  const heading = screen.getByRole('heading', { level: 2, name: /Battery & Power/i })
  expect(heading).toBeInTheDocument()
})

test('keeps a stale reading legible instead of fading it out', () => {
  render(
    <Tile title="Depth & Tide" stale staleLabel="1h 39m">
      <p>4.2</p>
    </Tile>,
  )

  const content = screen.getByText('4.2').parentElement
  expect(content).toHaveClass('grayscale')
  expect(content).not.toHaveClass('opacity-50')
})

test('gives a stale tile an amber outline instead of a quiet hairline', () => {
  const { container } = render(
    <Tile title="Depth & Tide" stale staleLabel="1h 39m">
      <p>content</p>
    </Tile>,
  )

  const card = container.querySelector('[data-stale="true"]')
  expect(card).toHaveClass('border-amber-500')
})

test('does not add an amber outline when the tile is live', () => {
  const { container } = render(
    <Tile title="Depth & Tide">
      <p>content</p>
    </Tile>,
  )

  const card = container.querySelector('[data-slot="card"]')
  expect(card).not.toHaveClass('border-amber-500')
})

test('sizes the stale badge at the sanctioned micro-label tier, not the map-annotation floor', () => {
  render(
    <Tile title="Depth & Tide" stale staleLabel="1h 39m">
      <p>content</p>
    </Tile>,
  )

  const badge = screen.getByTestId('tile-stale-badge')
  expect(badge).toHaveClass('text-[10px]')
  expect(badge).not.toHaveClass('text-[9px]')
})

test('shows the update age inside the badge itself, not only behind a hover tooltip', () => {
  render(
    <Tile title="Depth & Tide" stale staleLabel="1h 39m">
      <p>content</p>
    </Tile>,
  )

  const badge = screen.getByTestId('tile-stale-badge')
  expect(badge).toHaveTextContent('1h 39m')
})
