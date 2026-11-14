import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'

import { AssistantProposalCard } from '@/components/assistant-proposal-card'
import type { AssistantProposal } from '@/hooks/use-assistant-conversations'

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

// ADR 0289: Mate proposes a plan, and the card says what it would do. A new plan
// is one row per stop, an update is one sentence of what differs, and an applied
// plan offers Open plan.

const createCard = (overrides: Partial<AssistantProposal> = {}): AssistantProposal => ({
  id: 'p1',
  messageId: 'm1',
  status: 'pending',
  ops: [
    {
      type: 'plan',
      action: 'create',
      label: 'To The Boat Works',
      description: 'Add plan "To The Boat Works": 3 stops, Sun 15 Nov to Wed 18 Nov',
      after: {
        stops: [
          { arrive: '2026-11-15', arrive_label: 'Sun 15 Nov', nights: 1, name: 'Pelican Bay', lat: -25.5, lon: 152.9, source: 'saved place' },
          { arrive: '2026-11-16', arrive_label: 'Mon 16 Nov', nights: 2, name: 'Manly', lat: -27.45, lon: 153.19, source: 'place names' },
          { arrive: '2026-11-18', arrive_label: 'Wed 18 Nov', nights: 0, name: 'The Boat Works', lat: -27.88, lon: 153.35 },
        ],
      },
    },
  ],
  ...overrides,
})

describe('a plan proposal card', () => {
  it('a new plan is one row per stop: day, nights, place, where the position came from, and the position', () => {
    render(<AssistantProposalCard proposal={createCard()} canWrite onChange={vi.fn()} />)

    const rows = screen.getAllByRole('listitem').filter((li) => li.getAttribute('data-plan-stop') !== null)
    expect(rows).toHaveLength(3)
    expect(within(rows[0]).getByText('Pelican Bay')).toBeInTheDocument()
    expect(within(rows[0]).getByText('Sun 15 Nov · 1 night')).toBeInTheDocument()
    expect(within(rows[0]).getByText(/^saved place · 25°30\.000' S 152°54\.000' E/)).toBeInTheDocument()
    expect(within(rows[1]).getByText('Mon 16 Nov · 2 nights')).toBeInTheDocument()
    expect(within(rows[1]).getByText(/^place names · /)).toBeInTheDocument()
  })

  it('the last stop shows its date and no nights, and a stop with no source shows only its position', () => {
    render(<AssistantProposalCard proposal={createCard()} canWrite onChange={vi.fn()} />)
    const rows = screen.getAllByRole('listitem').filter((li) => li.getAttribute('data-plan-stop') !== null)
    expect(within(rows[2]).getByText('Wed 18 Nov')).toBeInTheDocument()
    expect(rows[2].textContent).not.toMatch(/night/)
    expect(within(rows[2]).getByText(/^27°52\.800' S/)).toBeInTheDocument()
  })

  it('the stops fill the card without a Stops label column', () => {
    render(<AssistantProposalCard proposal={createCard()} canWrite onChange={vi.fn()} />)
    expect(screen.queryByText('Stops')).not.toBeInTheDocument()
  })

  it('an update is its sentence alone, with no rows', () => {
    const update: AssistantProposal = {
      id: 'p2',
      messageId: 'm1',
      status: 'pending',
      ops: [
        {
          type: 'plan',
          action: 'update',
          label: 'To The Boat Works',
          description: 'Change plan "To The Boat Works": Manly: 1 night → 2 nights. Later stops move a day: The Boat Works arrives Wed 18 Nov.',
        },
      ],
    }
    render(<AssistantProposalCard proposal={update} canWrite onChange={vi.fn()} />)

    expect(screen.getByText(/Manly: 1 night → 2 nights\. Later stops move a day/)).toBeInTheDocument()
    expect(screen.queryByText('Stops')).not.toBeInTheDocument()
  })

  it('applied, it offers Open plan, which lands on the Forecast page with that plan chosen', () => {
    const applied = createCard({ status: 'applied' })
    applied.ops[0].href = '/forecast?plan=7'
    applied.ops[0].hrefLabel = 'Open plan'
    render(<AssistantProposalCard proposal={applied} canWrite onChange={vi.fn()} />)

    expect(screen.getByRole('link', { name: 'Open plan' })).toHaveAttribute('href', '/forecast?plan=7')
  })

  it('pending, it offers no Open plan', () => {
    render(<AssistantProposalCard proposal={createCard()} canWrite onChange={vi.fn()} />)
    expect(screen.queryByRole('link', { name: 'Open plan' })).not.toBeInTheDocument()
  })
})
