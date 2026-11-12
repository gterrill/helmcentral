import { renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import { inboxWaitingOnMate, useInbox } from '@/hooks/use-inbox'
import { resetQueryStore } from '@/lib/api-query'
import { stubApi } from './helpers/stub-api'

beforeEach(() => resetQueryStore())

const row = (id: string, over: Record<string, unknown> = {}) => ({
  id, filename: `${id}.jpg`, mime: 'image/jpeg', size_bytes: 1, capture_ms: null, bearing_deg: null, fingerprint: '', sharpness: 0,
  created_ms: 0, content_url: `/${id}`, group: { kind: 'no_time' }, gps_mismatch: false, suggested_kind: null, ...over,
})

describe('useInbox', () => {
  it('reports whether Mate is sorting alongside the items', async () => {
    stubApi({ 'GET /api/inbox': { items: [row('a', { suggested_kind: 'document' })], mate_sorting: true } })
    const { result } = renderHook(() => useInbox())
    await waitFor(() => expect(result.current.items).toHaveLength(1))
    expect(result.current.mateSorting).toBe(true)
  })

  it('reads a list from a box that does not say as Mate not sorting', async () => {
    stubApi({ 'GET /api/inbox': { items: [row('a')] } })
    const { result } = renderHook(() => useInbox())
    await waitFor(() => expect(result.current.items).toHaveLength(1))
    expect(result.current.mateSorting).toBe(false)
  })
})

describe('inboxWaitingOnMate', () => {
  it('is true while Mate is set up and an item has neither a kind nor a recorded failure', () => {
    expect(inboxWaitingOnMate({ mate_sorting: true, items: [row('a'), row('b', { suggested_kind: 'scenery' })] } as never)).toBe(true)
  })

  it('is false once every item is sorted or has failed, when Mate is off, and with nothing waiting', () => {
    expect(inboxWaitingOnMate({ mate_sorting: true, items: [row('a', { classify_error: 'x' }), row('b', { suggested_kind: 'scenery' })] } as never)).toBe(false)
    expect(inboxWaitingOnMate({ mate_sorting: false, items: [row('a')] } as never)).toBe(false)
    expect(inboxWaitingOnMate(undefined)).toBe(false)
  })
})
