import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { HotWaterTile } from '@/components/hot-water-tile'

test('renders the Hot Water tile with its static controls', () => {
  render(<HotWaterTile />)
  expect(screen.getAllByText('Hot Water').length).toBeGreaterThan(0)
  expect(screen.getByText('Off')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '1 HR' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '1.5 HR' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '2 HR' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'ON' })).toBeInTheDocument()
})
