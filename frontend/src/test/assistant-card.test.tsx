import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { AssistantCard } from '@/components/assistant-card'
import type { MateCard } from '@/lib/mate-cards'

describe('AssistantCard', () => {
  it('shows an error card, and does not throw, for a kind it has no case for', () => {
    // The compile-time check makes this impossible to write; a stale bundle or a
    // hand-built entry still can reach it, and one card must not blank the thread.
    const card = { kind: 'sonar_sweep' } as unknown as MateCard
    render(<AssistantCard card={card} />)
    const err = screen.getByTestId('assistant-card-error')
    expect(within(err).getByText(/unknown card kind "sonar_sweep"/)).toBeInTheDocument()
  })
})
