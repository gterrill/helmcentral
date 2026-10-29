import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '@/lib/api-query'
import { applyAssistantProposal, PROPOSAL_REFRESH } from '@/lib/assistant-proposal-actions'

// Apply writes records the open pages read. Each op type names the cached
// reads it changes, so a page already on screen reads them again.
const invalidated: string[] = []
vi.mock('@/lib/api-query', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api-query')>()
  return {
    ...actual,
    invalidateQueries: vi.fn((prefix: readonly unknown[]) => {
      invalidated.push(String(prefix[0]))
      return Promise.resolve()
    }),
  }
})

function proposalBody(types: string[]) {
  return {
    proposal: {
      id: 'p1',
      message_id: 'm1',
      status: 'applied',
      ops: types.map((type) => ({ type, action: 'create', description: 'x' })),
    },
  }
}

describe('applying a proposal refreshes the pages it changed', () => {
  beforeEach(() => {
    invalidated.length = 0
  })
  afterEach(() => vi.unstubAllGlobals())

  function reply(types: string[]) {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => proposalBody(types) }))
  }

  it('reads the decks and zones again after a deck is added', async () => {
    reply(['deck'])
    await applyAssistantProposal('p1')
    expect(invalidated).toContain('/api/inventory/decks')
    expect(invalidated).toContain('/api/inventory/zones')
  })

  it('reads zones again for a bin, which lives inside its zone', async () => {
    reply(['bin'])
    await applyAssistantProposal('p1')
    expect(invalidated).toContain('/api/inventory/zones')
  })

  it.each([
    ['equipment', '/api/inventory/equipment'],
    ['equipment', '/api/setup/status'],
    ['location', '/api/inventory/zones'],
    ['maintenance_rule', '/api/inventory/maintenance/rules'],
    ['maintenance_log', '/api/inventory/maintenance/log'],
    ['vessel_particulars', '/api/vessel/particulars'],
  ])('%s refreshes %s', async (type, path) => {
    reply([type])
    await applyAssistantProposal('p1')
    expect(invalidated).toContain(path)
  })

  it('refreshes the union of every op in the proposal', async () => {
    reply(['deck', 'vessel_particulars'])
    await applyAssistantProposal('p1')
    expect(invalidated).toContain('/api/inventory/decks')
    expect(invalidated).toContain('/api/vessel/particulars')
  })

  it('refreshes the same pages when the changes were saved but Apply reported an error', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: false, status: 500, json: async () => ({ error: 'reload failed', ...proposalBody(['deck']) }) }),
    )
    await expect(applyAssistantProposal('p1')).rejects.toBeInstanceOf(ApiError)
    expect(invalidated).toContain('/api/inventory/decks')
  })

  it('says so when a type has no entry, rather than skipping it quietly', async () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
    reply(['sprocket'])
    await applyAssistantProposal('p1')
    expect(spy).toHaveBeenCalledWith(expect.stringContaining('sprocket'))
    spy.mockRestore()
  })

  it('has a table entry for every op type', () => {
    expect(Object.keys(PROPOSAL_REFRESH).sort()).toEqual(
      ['alarm_rule', 'bin', 'deck', 'equipment', 'location', 'maintenance_log', 'maintenance_rule', 'planned_stay', 'vessel_particulars', 'watch'],
    )
  })
})
