import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { KioskFoldGuide } from '@/components/kiosk-fold-guide'

describe('KioskFoldGuide', () => {
  test('positions the line at the given offset and labels it', () => {
    render(<KioskFoldGuide topPx={344} />)
    const guide = screen.getByTestId('kiosk-fold')
    expect(guide.style.top).toBe('344px')
    expect(guide).toHaveTextContent('Kiosk fold, 360 px')
  })
})
