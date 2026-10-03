import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { AssistantProposalCard } from '@/components/assistant-proposal-card'
import { mapProposal, type AssistantProposal, type ProposalApi } from '@/hooks/use-assistant-conversations'

// ADR 0146, ADR 0158: the card under a Mate reply that proposes changes to
// records. It renders from the stored status, so every state
// below is also what a reloaded thread shows.

const proposal = (overrides: Partial<AssistantProposal> = {}): AssistantProposal => ({
  id: 'p1',
  messageId: 'm1',
  status: 'pending',
  ops: [
    {
      type: 'maintenance_rule',
      action: 'create',
      description: 'Add Generator · Oil and filter: every 250 h or 12 mo, last done 9 Jan 2025 at 239 h (meter)',
    },
    { type: 'maintenance_rule', action: 'update', description: 'Acknowledge Generator · Belts: parts on order' },
  ],
  ...overrides,
})

function respond(status: number, body: unknown) {
  return vi.fn().mockResolvedValue({ ok: status >= 200 && status < 300, status, json: async () => body })
}

describe('AssistantProposalCard', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(2026, 8, 30, 9, 0, 0))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('pending: shows one line per change and Apply and Dismiss', () => {
    render(<AssistantProposalCard proposal={proposal()} canWrite onChange={vi.fn()} />)

    expect(screen.getByText('Proposed changes (2)')).toBeInTheDocument()
    expect(screen.getByText(/Generator · Oil and filter: every 250 h or 12 mo, last done 9 Jan 2025 at 239 h \(meter\)/)).toBeInTheDocument()
    expect(screen.getByText('Acknowledge Generator · Belts: parts on order')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Apply' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Dismiss' })).toBeEnabled()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('Apply posts with the operator local date and hands the answer up', async () => {
    const applied = proposal({ status: 'applied' })
    const fetchMock = respond(200, {
      proposal: { id: 'p1', message_id: 'm1', status: 'applied', ops: applied.ops },
    })
    vi.stubGlobal('fetch', fetchMock)
    const onChange = vi.fn()
    render(<AssistantProposalCard proposal={proposal()} canWrite onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ id: 'p1', status: 'applied' })))
    expect(fetchMock).toHaveBeenCalledWith('/api/assistant/proposals/p1/apply?today=2026-09-30', { method: 'POST' })
  })

  it('Dismiss posts and hands the answer up', async () => {
    const fetchMock = respond(200, { proposal: { id: 'p1', message_id: 'm1', status: 'dismissed', ops: proposal().ops } })
    vi.stubGlobal('fetch', fetchMock)
    const onChange = vi.fn()
    render(<AssistantProposalCard proposal={proposal()} canWrite onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))

    await waitFor(() => expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ status: 'dismissed' })))
    expect(fetchMock).toHaveBeenCalledWith('/api/assistant/proposals/p1/dismiss', { method: 'POST' })
  })

  it('Apply refused with 409 because the proposal was dismissed shows as dismissed, not out of date', async () => {
    vi.stubGlobal(
      'fetch',
      respond(409, { error: 'this proposal was dismissed and can no longer be applied', proposal_status: 'dismissed' }),
    )
    const onChange = vi.fn()
    render(<AssistantProposalCard proposal={proposal()} canWrite onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ status: 'dismissed' })))
    expect(onChange).not.toHaveBeenCalledWith(expect.objectContaining({ status: 'stale' }))
  })

  it('applied: each change links to the page that shows its record and there are no buttons', () => {
    const base = proposal({ status: 'applied' })
    const ops = base.ops.map((op) => ({ ...op, href: '/inventory/maintenance' }))
    render(<AssistantProposalCard proposal={{ ...base, ops }} canWrite onChange={vi.fn()} />)

    expect(screen.getByText('Applied')).toBeInTheDocument()
    const links = screen.getAllByRole('link')
    expect(links).toHaveLength(2)
    for (const link of links) expect(link).toHaveAttribute('href', '/inventory/maintenance')
    expect(screen.queryByRole('button', { name: 'Apply' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Dismiss' })).not.toBeInTheDocument()
  })

  it('dismissed: shows what was proposed, marked dismissed, with no buttons or links', () => {
    render(<AssistantProposalCard proposal={proposal({ status: 'dismissed' })} canWrite onChange={vi.fn()} />)

    expect(screen.getByText('Dismissed')).toBeInTheDocument()
    expect(screen.getByText('Acknowledge Generator · Belts: parts on order')).toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('Apply answered 409 hands up a stale proposal carrying the server reason', async () => {
    vi.stubGlobal('fetch', respond(409, { error: 'a rule changed since Mate proposed this, so nothing was applied' }))
    const onChange = vi.fn()
    render(<AssistantProposalCard proposal={proposal()} canWrite onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() =>
      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({ status: 'stale', staleReason: 'a rule changed since Mate proposed this, so nothing was applied' }),
      ),
    )
  })

  it('stale (stored, so also after a reload): shows the reason and Ask Mate to redo it, with no buttons or links', () => {
    render(
      <AssistantProposalCard
        proposal={proposal({ status: 'stale', staleReason: 'a rule changed since Mate proposed this, so nothing was applied' })}
        canWrite
        onChange={vi.fn()}
      />,
    )

    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent('a rule changed since Mate proposed this, so nothing was applied')
    expect(alert).toHaveTextContent('Ask Mate to redo it.')
    expect(screen.getByText('Out of date')).toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('another failure shows the message and leaves Apply available to retry', async () => {
    vi.stubGlobal('fetch', respond(400, { field: 'description', message: 'change 1 of 2 (create_rule): description is required' }))
    render(<AssistantProposalCard proposal={proposal()} canWrite onChange={vi.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('description is required')
    expect(screen.getByRole('button', { name: 'Apply' })).toBeEnabled()
    expect(screen.queryByText(/Ask Mate to redo it/)).not.toBeInTheDocument()
  })

  it('read tier: pending changes are shown with no Apply or Dismiss', () => {
    render(<AssistantProposalCard proposal={proposal()} canWrite={false} onChange={vi.fn()} />)

    expect(screen.getByText('Acknowledge Generator · Belts: parts on order')).toBeInTheDocument()
    expect(screen.getByText(/Read-only session/)).toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('a single change reads as "Proposed change"', () => {
    render(<AssistantProposalCard proposal={proposal({ ops: [proposal().ops[0]] })} canWrite onChange={vi.fn()} />)

    expect(screen.getByText('Proposed change')).toBeInTheDocument()
  })

  it('an update shows each field it names, before and after', () => {
    const update: AssistantProposal = proposal({
      ops: [
        {
          type: 'equipment',
          action: 'update',
          description: 'Move Fuse kit to bin B-02 (Lazarette)',
          before: { bin_id: null, quantity: 1, verified_aboard: false },
          after: { bin_id: '0710cae2-01e8-4000-b421-2f4c23ceac15', quantity: 2, verified_aboard: true },
        },
      ],
    })
    render(<AssistantProposalCard proposal={update} canWrite onChange={vi.fn()} />)

    const bin = screen.getByText('Bin').closest('div') as HTMLElement
    expect(bin).toHaveTextContent('--')
    expect(bin).toHaveTextContent('#0710cae2')
    const quantity = screen.getByText('Quantity').closest('div') as HTMLElement
    expect(quantity).toHaveTextContent('1')
    expect(quantity).toHaveTextContent('2')
    expect(screen.getByText('Verified aboard').closest('div')).toHaveTextContent('No→Yes')
  })

  it('a create shows what it sets and a delete shows what goes with it', () => {
    const mixed: AssistantProposal = proposal({
      ops: [
        { type: 'bin', action: 'create', description: 'Add bin S-1', after: { code: 'S-1', name: 'Spares' } },
        {
          type: 'deck',
          action: 'delete',
          description: 'Delete deck Main (removes the outlines of 3 locations and the pins of their bins)',
          before: { name: 'Main' },
        },
      ],
    })
    render(<AssistantProposalCard proposal={mixed} canWrite onChange={vi.fn()} />)

    expect(screen.getByText('Code').closest('div')).toHaveTextContent('S-1')
    expect(screen.getByText('Add bin S-1')).toBeInTheDocument()
    expect(screen.getByText(/removes the outlines of 3 locations/)).toBeInTheDocument()
    expect(screen.queryByText('→')).not.toBeInTheDocument()
  })

  it('a local reference reads as the change that creates the record', () => {
    const refs: AssistantProposal = proposal({
      ops: [
        { type: 'deck', action: 'create', label: 'Main deck', description: 'Add deck Main deck', after: { name: 'Main deck' } },
        { type: 'location', action: 'update', description: 'Change location Salon', before: { deck_id: null }, after: { deck_id: '$1' } },
      ],
    })
    render(<AssistantProposalCard proposal={refs} canWrite onChange={vi.fn()} />)

    expect(screen.getByText('Deck').closest('div')).toHaveTextContent('Main deck')
  })

  it("a deck's plan is shown as a picture the operator can check", () => {
    const deck: AssistantProposal = proposal({
      ops: [
        {
          type: 'deck',
          action: 'create',
          description: 'Add deck Main with a plan picture',
          after: { name: 'Main', plan_document_id: 'doc-42' },
        },
      ],
    })
    render(<AssistantProposalCard proposal={deck} canWrite onChange={vi.fn()} />)

    const image = screen.getByRole('img', { name: 'Deck plan' })
    expect(image).toHaveAttribute('src', '/api/documents/doc-42/content')
  })
})

// The wire shape, as the server writes it for an applied changeset (captured
// from the backend, not assumed): mapProposal must carry every operation's
// fields and the page each record links to.
describe('AssistantProposalCard with the server payload', () => {
  const payload: ProposalApi = {
    id: 'p1',
    message_id: 'm1',
    status: 'applied',
    ops: [
      {
        type: 'deck',
        action: 'create',
        label: 'Main deck',
        description: 'Add deck Main deck with a plan picture',
        after: { name: 'Main deck', plan_document_id: '8bcd8d62-ba89-4e85-b3ac-5a63bdc7d31d' },
      },
      {
        type: 'location',
        action: 'update',
        id: 'f3638bc7-3776-4e75-a15d-446b4dd2c076',
        label: 'Salon',
        description: 'Change location Salon: put it on a deck plan with an outline',
        before: { deck_id: null, polygon: null },
        after: { deck_id: '$1', polygon: [[0.1, 0.1], [0.5, 0.1], [0.5, 0.5]] },
      },
    ],
    result: {
      ops: [
        { type: 'deck', action: 'create', id: 'c3b8', label: 'Main deck', href: '/inventory/decks/c3b8' },
        { type: 'location', action: 'update', id: 'f363', label: 'Salon', href: '/inventory/locations/f363' },
      ],
    },
  }

  it('links each applied change to its record and shows the plan and the outline', () => {
    render(<AssistantProposalCard proposal={mapProposal(payload)} canWrite onChange={vi.fn()} />)

    const links = screen.getAllByRole('link')
    expect(links.map((l) => l.getAttribute('href'))).toEqual(['/inventory/decks/c3b8', '/inventory/locations/f363'])
    expect(screen.getByRole('img', { name: 'Deck plan' })).toHaveAttribute(
      'src',
      '/api/documents/8bcd8d62-ba89-4e85-b3ac-5a63bdc7d31d/content',
    )
    expect(screen.getByText('Deck').closest('div')).toHaveTextContent('Main deck')
    expect(screen.getByText('Outline').closest('div')).toHaveTextContent('3 points')
  })
})
