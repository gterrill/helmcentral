import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { AssistantProposalCard } from '@/components/assistant-proposal-card'
import type { AssistantProposal } from '@/hooks/use-assistant-conversations'

// ADR 0146: the card under a Mate reply that proposes changes to the
// maintenance schedule. It renders from the stored status, so every state
// below is also what a reloaded thread shows.

const proposal = (overrides: Partial<AssistantProposal> = {}): AssistantProposal => ({
  id: 'p1',
  messageId: 'm1',
  status: 'pending',
  ops: [
    { op: 'create_rule', summary: 'Add Generator · Oil and filter: every 250 h or 12 mo, last done 9 Jan 2025 at 239 h (meter)' },
    { op: 'acknowledge', summary: 'Acknowledge Generator · Belts: parts on order' },
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

  it('applied: each line links to the Maintenance list and there are no buttons', () => {
    render(<AssistantProposalCard proposal={proposal({ status: 'applied' })} canWrite onChange={vi.fn()} />)

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
})
