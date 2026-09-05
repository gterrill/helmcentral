import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { CardTitle } from '@/components/ui/card'

test('CardTitle renders as a plain div by default, unchanged for existing callers', () => {
  render(<CardTitle>Settings</CardTitle>)

  const title = screen.getByText('Settings')
  expect(title.tagName).toBe('DIV')
})

test('CardTitle renders as an h2 when asked, so a tile title becomes a landmark', () => {
  render(<CardTitle as="h2">Battery &amp; Power</CardTitle>)

  expect(screen.getByRole('heading', { level: 2, name: /Battery & Power/i })).toBeInTheDocument()
})
