import { act } from '@testing-library/react'
import { setQueryData } from '@/lib/api-query'

/**
 * Lands one GET /api/anchor-watch answer in the query store, as the poll would,
 * so the real useAnchorWatch hooks read it. The store is cleared after each
 * test (src/test/setup.ts). With no argument the server says no watch is set.
 */
export function landAnchorWatch(server: Record<string, unknown> = { active: false }): void {
  act(() => { setQueryData(['/api/anchor-watch'], server) })
}
