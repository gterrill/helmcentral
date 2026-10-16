import { describe, expect, test } from 'vitest'

import { CLUSTER_FOOTER_SLOTS } from '@/lib/dashboard-widgets'

/**
 * The footer's slot order fixes the index of the alarm ids of zones a slot still
 * holds, so the backend and the frontend must list the same slots in the same
 * order. backend/testdata/cluster_footer_slots.json is read by this test and by
 * the Go one, so neither can drift on its own.
 */
describe('the footer slot order', () => {
  test('is the backend fixture, with no gap', async () => {
    const [fs, path] = await Promise.all([import('node:fs'), import('node:path')])
    const file = path.resolve(process.cwd(), '../backend/testdata/cluster_footer_slots.json')
    expect([...CLUSTER_FOOTER_SLOTS]).toEqual(JSON.parse(fs.readFileSync(file, 'utf8')))
  })
})
