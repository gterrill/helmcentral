import { describe, expect, it } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'

import { useEquipmentDependants } from '@/hooks/use-equipment-dependants'
import { stubApi } from '@/test/helpers/stub-api'

const body = (n: number) => ({ item_id: 'a', tiles: [], roles: [], alarm_rules: [], features: Array.from({ length: n }, (_, i) => `f${i}`) })

describe('useEquipmentDependants', () => {
  it('does not show an earlier answer for the same item while the next one is still loading', async () => {
    let reads = 0
    let release: () => void = () => {}
    stubApi({
      'GET /api/inventory/equipment/a/dependants': async () => {
        reads += 1
        if (reads === 2) await new Promise<void>((resolve) => { release = resolve })
        return body(reads)
      },
    })
    const { result, rerender } = renderHook(({ id }: { id: string | null }) => useEquipmentDependants(id), { initialProps: { id: 'a' as string | null } })
    await waitFor(() => expect(result.current.status).toBe('ready'))

    rerender({ id: null })
    expect(result.current.status).toBe('idle')
    rerender({ id: 'a' })
    // The first answer is stale now: loading, so Confirm stays off.
    expect(result.current.status).toBe('loading')
    await act(async () => { release() })
    await waitFor(() => expect(result.current.status).toBe('ready'))
    if (result.current.status === 'ready') expect(result.current.dependants.features).toHaveLength(2)
  })
})
