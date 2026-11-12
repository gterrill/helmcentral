import { beforeEach, describe, expect, it } from 'vitest'

import { resetQueryStore } from '@/lib/api-query'
import { inboxApi, type InboxUploadMeta } from '@/lib/inbox-api'
import { jsonResponse, stubApi } from './helpers/stub-api'

beforeEach(() => resetQueryStore())

const meta = (over: Partial<InboxUploadMeta> = {}): InboxUploadMeta => ({
  captureMs: 1000, bearingDeg: 80, phoneLat: null, phoneLon: null, fingerprint: '', sharpness: 0, sourceSha256: '', ...over,
})

describe('inboxApi.upload', () => {
  it('sends which north the bearing is measured from beside the bearing', async () => {
    const mock = stubApi({ 'POST /api/inbox': () => jsonResponse({ status: 'stored', item: {} }, 201) })
    await inboxApi.upload(new Blob(['x']), 'a.jpg', meta({ bearingRef: 'M' }))
    const form = mock.mock.calls[0][1]!.body as FormData
    expect(form.get('bearing_deg')).toBe('80')
    expect(form.get('bearing_ref')).toBe('M')
  })

  it('sends no reference when the phone gave none', async () => {
    const mock = stubApi({ 'POST /api/inbox': () => jsonResponse({ status: 'stored', item: {} }, 201) })
    await inboxApi.upload(new Blob(['x']), 'a.jpg', meta({ bearingRef: null }))
    await inboxApi.upload(new Blob(['x']), 'b.jpg', meta({ bearingDeg: null }))
    for (const call of mock.mock.calls) expect((call[1]!.body as FormData).has('bearing_ref')).toBe(false)
  })
})

describe('inboxApi.suggestedTitles', () => {
  it('returns the titles and the reasons titles could not be made', async () => {
    stubApi({
      'GET /api/inbox/suggested-titles': () => ({ titles: { a: 'Dolphins off the bow', b: '' }, errors: { b: 'Mate could not suggest a title: 429' } }),
    })
    expect(await inboxApi.suggestedTitles(['a', 'b'])).toEqual({
      titles: { a: 'Dolphins off the bow', b: '' },
      errors: { b: 'Mate could not suggest a title: 429' },
    })
  })
})
