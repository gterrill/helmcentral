import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { fieldLabel, formatFieldValue } from '@/components/assistant-proposal-views'
import { useWatchList } from '@/hooks/use-watches'
import { applyAssistantProposal } from '@/lib/assistant-proposal-actions'
import { resetQueryStore } from '@/lib/api-query'

// ADR 0227: Mate proposes a watch. The card names its fields the way the
// Watches page does.
describe('watch proposal field labels', () => {
  it('reads the schedule, sections and thresholds in the operator’s words', () => {
    expect(fieldLabel('schedule', 'watch')).toBe('Runs')
    expect(fieldLabel('sections', 'watch')).toBe('Reports on')
    expect(fieldLabel('look_out_for', 'watch')).toBe('Looks out for')
    expect(fieldLabel('notify', 'watch')).toBe('Sends to')
    expect(fieldLabel('send', 'watch')).toBe('Sends')
    expect(fieldLabel('tank_change_points', 'watch')).toBe('Tank change that counts (points)')
    expect(fieldLabel('pressure_fall_hpa', 'watch')).toBe('Pressure fall that counts (hPa)')
  })

  it('reads send and notify as the page does, not as stored', () => {
    expect(formatFieldValue('findings', [], 'watch', 'send')).toBe('Only when it finds something')
    expect(formatFieldValue('every', [], 'watch', 'send')).toBe('Every report')
    expect(formatFieldValue(['ntfy', 'smtp', 'webpush'], [], 'watch', 'notify')).toBe('ntfy, Email, Phone and browser push')
    expect(formatFieldValue([], [], 'watch', 'notify')).toBe('--')
  })

  it('leaves the same words alone on other types', () => {
    expect(formatFieldValue('findings', [], 'location', 'send')).toBe('findings')
    expect(formatFieldValue(['ntfy'], [], undefined, undefined)).toBe('ntfy')
  })

  it('leaves other types alone', () => {
    expect(fieldLabel('schedule', 'location')).toBe('Schedule')
  })
})

// A watch Mate proposed is on the Watches page the moment the operator taps Apply.
describe('applying a watch proposal', () => {
  const proposal = (type: string) => ({
    id: 'p1',
    message_id: 'm1',
    status: 'applied',
    ops: [{ type, action: 'create', label: 'Morning report', description: '', fields: [] }],
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    resetQueryStore()
  })

  async function applyAndCountWatchReads(type: string): Promise<number> {
    let watchReads = 0
    const calls: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      calls.push(String(url))
      if (String(url).includes('/proposals/')) return { ok: true, status: 200, json: async () => ({ proposal: proposal(type) }) }
      if (String(url).includes('/api/watches')) {
        watchReads += 1
        return { ok: true, status: 200, json: async () => ({ watches: [], max: 20, cost_24h: { usd: 0, unknown_runs: 0 }, storage_bytes: 0 }) }
      }
      return { ok: false, status: 404, json: async () => ({}) }
    }))
    const { result, unmount } = renderHook(() => useWatchList())
    await waitFor(() => expect(result.current.loading).toBe(false))
    const before = watchReads
    await applyAssistantProposal('p1')
    await waitFor(() => expect(watchReads).toBeGreaterThan(before))
    unmount()
    return watchReads - before
  }

  it('reads the watches again', async () => {
    expect(await applyAndCountWatchReads('watch')).toBeGreaterThan(0)
  })
})
