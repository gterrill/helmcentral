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

// This is the test that would have caught the original twMerge bug: Tile passed
// `flex-row` to win over CardHeader's base `grid`, but tailwind-merge treats
// `display` (grid) and `flex-direction` (flex-row) as different groups, so both
// classes survived and `grid` — being later in the base string's cascade order —
// still applied. The header rendered as a two-row grid instead of one flex row.
// Only a bare `flex` is in the same tailwind-merge group as `grid` and can
// actually replace it.
test('resolves the header to a flex row, not a grid, so the title and rule sit on one line', () => {
  const { container } = render(
    <Tile title="Depth">
      <p>content</p>
    </Tile>,
  )

  const header = container.querySelector('[data-slot="card-header"]')
  expect(header).toHaveClass('flex')
  expect(header?.className.split(/\s+/)).not.toContain('grid')
})

test('carries the tightened card and header padding, not the old wider defaults', () => {
  const { container } = render(
    <Tile title="Depth">
      <p>content</p>
    </Tile>,
  )

  const card = container.querySelector('[data-slot="card"]')
  expect(card).toHaveClass('py-2')
  expect(card?.className.split(/\s+/)).not.toContain('py-4')

  const header = container.querySelector('[data-slot="card-header"]')
  expect(header).toHaveClass('pb-2')
  expect(header?.className.split(/\s+/)).not.toContain('pb-3')
})
