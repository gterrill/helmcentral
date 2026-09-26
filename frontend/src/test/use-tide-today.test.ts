import { describe, it, expect, afterEach, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useTideToday } from '@/hooks/use-tide-today'

// Code-review findings (v0.33.0..main): the hook's own coercion of the
// backend's tide-today payload used to (1) collapse any height below -1 ft
// into the -1 "not published" sentinel, which misreads a real spring low
// below about -0.3 m datum as missing data, and (2) substitute
// `new Date().toISOString()` for a missing high/low time, which invents a
// time an operator reads as real (a fake "High · <now>"/"Low <tomorrow>").
// The fix: only a non-number becomes the -1 height sentinel (any finite
// number, however negative, is real), and a missing/non-string time stays
// '' rather than becoming "now".
describe('useTideToday', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('keeps a real height below -1 ft rather than coercing it to the sentinel', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        datetime: '2026-09-26T10:00:00Z',
        current_tide_height_ft: -1.5,
        tide_direction: 'Falling',
        high_tide_time: '2026-09-26T04:00:00Z',
        high_tide_height_ft: 5,
        low_tide_time: '2026-09-26T16:00:00Z',
        low_tide_height_ft: -1.2,
        station_name: 'Test Station',
        provider: 'test',
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useTideToday())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.tide.current_tide_height_ft).toBe(-1.5)
    expect(result.current.tide.low_tide_height_ft).toBe(-1.2)
  })

  it('still coerces a non-number height to the -1 sentinel', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        datetime: '2026-09-26T10:00:00Z',
        current_tide_height_ft: 'not a number',
        tide_direction: 'Falling',
        high_tide_time: '2026-09-26T04:00:00Z',
        high_tide_height_ft: null,
        low_tide_time: '',
        low_tide_height_ft: undefined,
        station_name: 'Test Station',
        provider: 'test',
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useTideToday())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.tide.current_tide_height_ft).toBe(-1)
    expect(result.current.tide.high_tide_height_ft).toBe(-1)
    expect(result.current.tide.low_tide_height_ft).toBe(-1)
  })

  it('keeps a missing high/low time as an empty string rather than inventing "now"', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        datetime: '2026-09-26T10:00:00Z',
        current_tide_height_ft: 2,
        tide_direction: 'Falling',
        high_tide_time: '2026-09-26T04:00:00Z',
        high_tide_height_ft: 5,
        low_tide_time: '',
        low_tide_height_ft: -1,
        station_name: 'Test Station',
        provider: 'test',
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useTideToday())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.tide.low_tide_time).toBe('')
  })
})
