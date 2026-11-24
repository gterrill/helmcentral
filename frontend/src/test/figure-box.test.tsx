import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { FigureBox } from '@/components/ui/figure-box'

// tailwind-merge drops a line height that comes before a font size, so a
// caller's size class must not take the figure's leading-none with it.
test('a figure keeps leading-none when the caller sets its size', () => {
  render(<FigureBox label="Today" value="12.54" unit="kWh" valueClassName="text-2xl text-gauge-primary" />)
  const figure = screen.getByText('12.54')
  expect(figure).toHaveClass('leading-none')
  expect(figure).toHaveClass('@max-2xs:leading-none')
})

test('a caller that names its own line height keeps it', () => {
  render(<FigureBox label="Runtime" value="0:10:00" valueClassName="text-2xl leading-8" />)
  const figure = screen.getByText('0:10:00')
  expect(figure).toHaveClass('leading-8')
  expect(figure).not.toHaveClass('leading-none')
})
