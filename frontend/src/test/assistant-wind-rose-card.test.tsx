import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { AssistantCard } from '@/components/assistant-card'
import { AssistantWindRoseCard } from '@/components/assistant-wind-rose-card'

import { windRoseCard } from './helpers/wind-rose-fixture'

describe('AssistantWindRoseCard', () => {
  it('shows the Wind rose label, title and the range', () => {
    render(<AssistantWindRoseCard card={windRoseCard()} />)
    expect(screen.getByText('Wind rose')).toBeInTheDocument()
    expect(screen.getByText('Where the wind came from, 21 Oct to 28 Oct')).toBeInTheDocument()
    expect(screen.getByText('21–28 Oct')).toBeInTheDocument()
  })

  it('draws one wedge for each band of each sector that has wind', () => {
    const { container } = render(<AssistantWindRoseCard card={windRoseCard()} />)
    // East has three bands with wind, south three.
    expect(container.querySelectorAll('[data-testid="rose-wedge"]')).toHaveLength(6)
  })

  it('describes each wedge for assistive tech and on hover', () => {
    const { container } = render(<AssistantWindRoseCard card={windRoseCard()} />)
    const titles = [...container.querySelectorAll('[data-testid="rose-wedge"] title')].map((t) => t.textContent)
    expect(titles).toContain('E, 10–15 kn: 30%')
    expect(titles).toContain('S, 5–10 kn: 10%')
  })

  it('labels the rose for assistive tech with where the wind came from', () => {
    render(<AssistantWindRoseCard card={windRoseCard()} />)
    expect(screen.getByRole('img', { name: /Wind rose.*mostly from E.*60%/ })).toBeInTheDocument()
  })

  it('states the prevailing direction, calm and variable shares, mean and strongest gust', () => {
    render(<AssistantWindRoseCard card={windRoseCard()} />)
    const facts = screen.getByTestId('rose-facts')
    expect(within(facts).getByText(/Mostly from E, 60% of the time/)).toBeInTheDocument()
    expect(within(facts).getByText(/Calm 10%/)).toBeInTheDocument()
    expect(within(facts).getByText(/under 1 kn/)).toBeInTheDocument()
    expect(within(facts).getByText(/Variable 5%/)).toBeInTheDocument()
    expect(within(facts).getByText(/Mean 11.4 kn/)).toBeInTheDocument()
    expect(within(facts).getByText(/Strongest 24.6 kn, Sat 24 Oct 16:10/)).toBeInTheDocument()
    expect(within(facts).getByText(/10-minute averages/)).toBeInTheDocument()
  })

  it('leaves out what it does not know', () => {
    const counts = Array.from({ length: 16 }, () => [0, 0, 0, 0, 0, 0])
    render(<AssistantWindRoseCard card={windRoseCard({ counts, samples: 10, calm: 6, variable: 4, mean_speed: null, strongest: null })} />)
    const facts = screen.getByTestId('rose-facts')
    expect(within(facts).queryByText(/Mostly from/)).toBeNull()
    expect(within(facts).queryByText(/Strongest/)).toBeNull()
    expect(within(facts).queryByText(/Mean/)).toBeNull()
    expect(within(facts).getByText(/No steady wind direction/)).toBeInTheDocument()
  })

  it('says how many time buckets had only one of direction and speed', () => {
    render(<AssistantWindRoseCard card={windRoseCard({ unpaired: 14 })} />)
    expect(screen.getByText(/14 buckets had direction or speed but not both/)).toBeInTheDocument()
  })

  it('lists the speed bands in a legend', () => {
    render(<AssistantWindRoseCard card={windRoseCard()} />)
    const legend = screen.getByTestId('rose-legend')
    for (const label of ['0–5 kn', '10–15 kn', '25+ kn']) expect(within(legend).getByText(label)).toBeInTheDocument()
  })

  it('names the four compass points on the rim, at the permitted label size and a token colour', () => {
    const { container } = render(<AssistantWindRoseCard card={windRoseCard()} />)
    const labels = [...container.querySelectorAll('svg text')]
    expect(labels.map((t) => t.textContent).filter((t) => ['N', 'E', 'S', 'W'].includes(t ?? ''))).toEqual(['N', 'E', 'S', 'W'])
    for (const t of labels) {
      expect(t.getAttribute('font-size')).toBe('10')
      expect(t.getAttribute('fill')).toMatch(/^hsl\(var\(--[a-z-]+\)\)$/)
    }
  })

  it('draws every wedge in a theme token, never a raw colour', () => {
    const { container } = render(<AssistantWindRoseCard card={windRoseCard()} />)
    for (const w of container.querySelectorAll('[data-testid="rose-wedge"]')) {
      expect(w.getAttribute('fill')).toMatch(/^hsl\(var\(--[a-z-]+\)\)$/)
    }
  })

  it('draws no wedges and no division by zero when nothing was counted', () => {
    const counts = Array.from({ length: 16 }, () => [0, 0, 0, 0, 0, 0])
    const { container } = render(<AssistantWindRoseCard card={windRoseCard({ counts, samples: 3, calm: 3, variable: 0 })} />)
    expect(container.querySelectorAll('[data-testid="rose-wedge"]')).toHaveLength(0)
    expect(container.innerHTML).not.toContain('NaN')
  })
})

describe('AssistantCard: wind_rose', () => {
  it('renders the wind rose card', async () => {
    render(<AssistantCard card={windRoseCard()} />)
    expect(await screen.findByTestId('assistant-wind-rose-card')).toBeInTheDocument()
  })
})
