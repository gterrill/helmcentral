import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { toast } from 'sonner'

import { AssistantProposalCard } from '@/components/assistant-proposal-card'
import { mapProposal, type AssistantProposal, type ProposalApi } from '@/hooks/use-assistant-conversations'

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

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
    vi.mocked(toast.success).mockClear()
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
    expect(fetchMock).toHaveBeenCalledWith('/api/assistant/proposals/p1/apply?today=2026-09-30', expect.objectContaining({ method: 'POST' }))
  })

  it('Apply confirms with a toast that counts the changes', async () => {
    vi.stubGlobal('fetch', respond(200, { proposal: { id: 'p1', message_id: 'm1', status: 'applied', ops: proposal().ops } }))
    render(<AssistantProposalCard proposal={proposal()} canWrite onChange={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('Applied 2 changes'))
  })

  it('Apply confirms a single change in the singular', async () => {
    const one = proposal({ ops: proposal().ops.slice(0, 1) })
    vi.stubGlobal('fetch', respond(200, { proposal: { id: 'p1', message_id: 'm1', status: 'applied', ops: one.ops } }))
    render(<AssistantProposalCard proposal={one} canWrite onChange={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('Applied 1 change'))
  })

  it('Dismiss does not toast', async () => {
    vi.stubGlobal('fetch', respond(200, { proposal: { id: 'p1', message_id: 'm1', status: 'dismissed', ops: proposal().ops } }))
    const onChange = vi.fn()
    render(<AssistantProposalCard proposal={proposal()} canWrite onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    await waitFor(() => expect(onChange).toHaveBeenCalled())
    expect(toast.success).not.toHaveBeenCalled()
  })

  it('Apply saved with an error shows no success toast', async () => {
    vi.stubGlobal('fetch', respond(500, { error: 'rules did not reload', proposal: { id: 'p1', message_id: 'm1', status: 'applied', ops: proposal().ops } }))
    render(<AssistantProposalCard proposal={proposal()} canWrite onChange={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))
    await screen.findByText(/rules did not reload/)
    expect(toast.success).not.toHaveBeenCalled()
  })

  it('Dismiss posts and hands the answer up', async () => {
    const fetchMock = respond(200, { proposal: { id: 'p1', message_id: 'm1', status: 'dismissed', ops: proposal().ops } })
    vi.stubGlobal('fetch', fetchMock)
    const onChange = vi.fn()
    render(<AssistantProposalCard proposal={proposal()} canWrite onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))

    await waitFor(() => expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ status: 'dismissed' })))
    expect(fetchMock).toHaveBeenCalledWith('/api/assistant/proposals/p1/dismiss', expect.objectContaining({ method: 'POST' }))
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

  it('a document proposal shows its tags before and after, and links to the document once applied', () => {
    const op = {
      type: 'document',
      action: 'update' as const,
      description: 'On Berth map: add tag "Coral Sea Marina Resort"',
      before: { tags: ['map'] },
      after: { tags: ['map', 'Coral Sea Marina Resort'] },
    }
    const { rerender } = render(<AssistantProposalCard proposal={proposal({ ops: [op] })} canWrite onChange={vi.fn()} />)

    const tags = screen.getByText('Tags').closest('div') as HTMLElement
    expect(tags).toHaveTextContent('map→map, Coral Sea Marina Resort')
    expect(screen.getByText('On Berth map: add tag "Coral Sea Marina Resort"')).toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()

    rerender(<AssistantProposalCard proposal={proposal({ status: 'applied', ops: [{ ...op, href: '/documents/d1' }] })} canWrite onChange={vi.fn()} />)
    expect(screen.getByRole('link', { name: /On Berth map/ })).toHaveAttribute('href', '/documents/d1')
  })

  describe('a kept fact', () => {
    const kept: AssistantProposal = {
      id: 'p9',
      messageId: 'm9',
      status: 'applied',
      ops: [{ type: 'memory', action: 'create', description: 'Keep this fact: The polisher has no port.', resultId: 'mem-1' }],
    }

    it('saved at the skipper\'s asking: Undo is a server action on the proposal and hands the answer up', async () => {
      const fetchMock = respond(200, { proposal: { id: 'p9', message_id: 'm9', status: 'undone', ops: kept.ops, result: { ops: [{ type: 'memory', action: 'create', id: 'mem-1' }] } } })
      vi.stubGlobal('fetch', fetchMock)
      const onChange = vi.fn()
      render(<AssistantProposalCard proposal={kept} canWrite onChange={onChange} />)

      fireEvent.click(screen.getByRole('button', { name: 'Undo' }))

      await waitFor(() => expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ id: 'p9', status: 'undone' })))
      expect(fetchMock.mock.calls[0][0]).toBe('/api/assistant/proposals/p9/undo')
      expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: 'POST' })
    })

    it('an undone card, as a reload shows it, reads Undone with no Undo and no Applied beside Removed', () => {
      render(<AssistantProposalCard proposal={{ ...kept, status: 'undone' }} canWrite onChange={vi.fn()} />)

      expect(screen.getByText('Removed from Mate\'s memory')).toBeInTheDocument()
      expect(screen.getByText('Undone')).toBeInTheDocument()
      expect(screen.queryByText('Applied')).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()
    })

    it('a fact that could not be kept: says Not kept and why, with no Undo and no redo prompt', () => {
      const reason = 'Not kept. That fact was already kept.'
      render(<AssistantProposalCard proposal={{ ...kept, status: 'stale', staleReason: reason }} canWrite onChange={vi.fn()} />)

      expect(screen.getByText('Not kept')).toBeInTheDocument()
      expect(screen.getByRole('alert')).toHaveTextContent(reason)
      expect(screen.getByRole('alert')).not.toHaveTextContent('Ask Mate to redo it')
      expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()
    })

    it('a fact rewritten since it was kept: shows the server\'s plain reason and keeps the card as it was', async () => {
      const reason = 'that fact was changed after Mate kept it, so Undo left it alone.'
      vi.stubGlobal('fetch', respond(409, { error: reason, proposal_status: 'applied' }))
      const onChange = vi.fn()
      render(<AssistantProposalCard proposal={kept} canWrite onChange={onChange} />)

      fireEvent.click(screen.getByRole('button', { name: 'Undo' }))

      await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(reason))
      expect(onChange).not.toHaveBeenCalled()
      expect(screen.getByRole('button', { name: 'Undo' })).toBeEnabled()
    })

    it('read-only sessions get no Undo', () => {
      render(<AssistantProposalCard proposal={kept} canWrite={false} onChange={vi.fn()} />)
      expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()
    })

    it('a fact Mate offers waits on Remember this, and tapping it applies the proposal', async () => {
      const fetchMock = respond(200, { proposal: { ...kept, status: 'applied' } })
      vi.stubGlobal('fetch', fetchMock)
      const offered: AssistantProposal = { ...kept, status: 'pending' }
      render(<AssistantProposalCard proposal={offered} canWrite onChange={vi.fn()} />)

      expect(screen.queryByRole('button', { name: 'Apply' })).not.toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: 'Remember this' }))

      await waitFor(() => expect(fetchMock).toHaveBeenCalled())
      expect(String(fetchMock.mock.calls[0][0])).toContain('/apply')
    })

    it('a replacement shows the old and the new text and waits for Replace fact', () => {
      const replacing: AssistantProposal = {
        id: 'p10',
        messageId: 'm10',
        status: 'pending',
        ops: [{
          type: 'memory',
          action: 'update',
          id: 'mem-1',
          description: 'Replace the kept fact with: The polisher has no sampling port.',
          before: { text: 'The polisher has a sampling port.' },
          after: { text: 'The polisher has no sampling port.' },
        }],
      }
      render(<AssistantProposalCard proposal={replacing} canWrite onChange={vi.fn()} />)

      expect(screen.getByText(/The polisher has a sampling port\./)).toBeInTheDocument()
      expect(screen.getAllByText(/The polisher has no sampling port\./).length).toBeGreaterThan(0)
      expect(screen.getByRole('button', { name: 'Replace fact' })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()
    })

    it('maps the id an applied save wrote from the stored result', () => {
      const api: ProposalApi = {
        id: 'p9', message_id: 'm9', status: 'applied',
        ops: [{ type: 'memory', action: 'create', description: 'Keep this fact: x' }],
        result: { ops: [{ type: 'memory', action: 'create', id: 'mem-1' }] },
      }
      expect(mapProposal(api).ops[0].resultId).toBe('mem-1')
    })
  })
})
