import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { DisplayFoldGuide } from '@/components/display-fold-guide'

describe('DisplayFoldGuide', () => {
  test('positions the line at the given offset and labels it with the display name and height', () => {
    render(<DisplayFoldGuide topPx={344} heightPx={360} displayName="Flybridge" />)
    const guide = screen.getByTestId('display-fold')
    expect(guide.style.top).toBe('344px')
    expect(guide).toHaveTextContent('Flybridge fold, 360 px')
  })

  test('is not hardcoded to the flybridge strip - a second display renders its own name and height', () => {
    render(<DisplayFoldGuide topPx={704} heightPx={720} displayName="Saloon TV" />)
    const guide = screen.getByTestId('display-fold')
    expect(guide.style.top).toBe('704px')
    expect(guide).toHaveTextContent('Saloon TV fold, 720 px')
  })
})
