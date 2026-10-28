import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { AssistantProposalCard } from '@/components/assistant-proposal-card'
import type { AssistantProposal } from '@/hooks/use-assistant-conversations'

// ADR 0225: Mate proposes an alarm rule. The card shows it in the operator's
// units, and applying it makes every open Alarms page read the rules again.

const invalidate = vi.hoisted(() => vi.fn().mockResolvedValue(undefined))
vi.mock('@/lib/api-query', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/api-query')>()),
  invalidateQueries: invalidate,
}))

const proposal: AssistantProposal = {
  id: 'p1',
  messageId: 'm1',
  status: 'pending',
  ops: [
    {
      type: 'alarm_rule',
      action: 'update',
      description: 'Change alarm House Bank Low (built-in rule): now a warning when it falls below 40 %, clearing above 45 %, after 30 s',
      before: { value: '50 %', clear_value: '55 %' },
      after: { value: '40 %', clear_value: '45 %' },
    },
  ],
}

afterEach(() => {
  vi.unstubAllGlobals()
  invalidate.mockClear()
})

describe('alarm rule proposal card', () => {
  it('shows threshold and clear point with their units under operator labels', () => {
    render(<AssistantProposalCard proposal={proposal} canWrite onChange={vi.fn()} />)

    expect(screen.getByText(/now a warning when it falls below 40 %, clearing above 45 %/)).toBeInTheDocument()
    expect(screen.getByText('Threshold')).toBeInTheDocument()
    expect(screen.getByText('Clears at')).toBeInTheDocument()
    expect(screen.getByText('50 %')).toBeInTheDocument()
    expect(screen.getByText('40 %')).toBeInTheDocument()
  })

  it('keeps the alarm labels to alarm changes', () => {
    const other: AssistantProposal = {
      ...proposal,
      ops: [{ type: 'equipment', action: 'update', description: 'Change X', before: { value: 'a' }, after: { value: 'b' } }],
    }
    render(<AssistantProposalCard proposal={other} canWrite onChange={vi.fn()} />)
    expect(screen.getByText('Value')).toBeInTheDocument()
    expect(screen.queryByText('Threshold')).not.toBeInTheDocument()
  })

  it('Apply reads the alarm rules again', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ proposal: { id: 'p1', message_id: 'm1', status: 'applied', ops: proposal.ops } }) }),
    )
    const onChange = vi.fn()
    render(<AssistantProposalCard proposal={proposal} canWrite onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => expect(onChange).toHaveBeenCalled())
    expect(invalidate).toHaveBeenCalledWith(['/api/alarm-rules'])
  })
})
