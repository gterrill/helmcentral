import { beforeEach, describe, expect, it } from 'vitest'

import { resetQueryStore } from '@/lib/api-query'
import { inboxApi, type InboxUploadMeta } from '@/lib/inbox-api'
import { jsonResponse, stubApi } from './helpers/stub-api'

beforeEach(() => resetQueryStore())

const meta = (over: Partial<InboxUploadMeta> = {}): InboxUploadMeta => ({
  captureMs: 1000, bearingDeg: 80, phoneLat: null, phoneLon: null, fingerprint: '', sharpness: 0, sourceSha256: '', ...over,
})

describe('inboxApi.upload', () => {
  it('sends the bearing as it is, and no reference to which north it is from', async () => {
    const mock = stubApi({ 'POST /api/inbox': () => jsonResponse({ status: 'stored', item: {} }, 201) })
    await inboxApi.upload(new Blob(['x']), 'a.jpg', meta())
    await inboxApi.upload(new Blob(['x']), 'b.jpg', meta({ bearingDeg: null }))
    const first = mock.mock.calls[0][1]!.body as FormData
    expect(first.get('bearing_deg')).toBe('80')
    expect((mock.mock.calls[1][1]!.body as FormData).has('bearing_deg')).toBe(false)
    for (const call of mock.mock.calls) expect((call[1]!.body as FormData).has('bearing_ref')).toBe(false)
  })
})

describe('inboxApi.sortAgain', () => {
  it('asks the box to look at the failed items again and says how many it requeued', async () => {
    const mock = stubApi({ 'POST /api/inbox/sort-again': () => jsonResponse({ requeued: 3 }) })
    expect(await inboxApi.sortAgain()).toBe(3)
    expect(mock.mock.calls.map(([u, init]) => `${init?.method} ${new URL(String(u), 'http://x').pathname}`)).toEqual(['POST /api/inbox/sort-again'])
  })

  it('says why when the box refuses', async () => {
    stubApi({ 'POST /api/inbox/sort-again': () => jsonResponse({ error: 'the Inbox is not running' }, 503) })
    await expect(inboxApi.sortAgain()).rejects.toThrow('the Inbox is not running')
  })
})

describe('inboxApi.suggestedTitles', () => {
  it('returns the titles and the reasons titles could not be made', async () => {
    stubApi({
      'POST /api/inbox/suggested-titles': () => ({ titles: { a: 'Dolphins off the bow', b: '' }, errors: { b: 'Mate could not suggest a title: 429' } }),
    })
    expect(await inboxApi.suggestedTitles(['a', 'b'])).toEqual({
      titles: { a: 'Dolphins off the bow', b: '' },
      errors: { b: 'Mate could not suggest a title: 429' },
    })
  })

  it('asks with a POST and the ids in the body, since it can spend money', async () => {
    const mock = stubApi({ 'POST /api/inbox/suggested-titles': () => ({ titles: {}, errors: {} }) })
    await inboxApi.suggestedTitles(['a', 'b'])
    const [url, init] = mock.mock.calls[0]
    expect(init?.method).toBe('POST')
    expect(String(url)).not.toContain('ids=')
    expect(JSON.parse(String(init?.body))).toEqual({ ids: ['a', 'b'] })
  })

  it('passes on the notice that Mate\'s settings could not be read', async () => {
    stubApi({
      'POST /api/inbox/suggested-titles': () => ({ titles: { a: 'Hook Island' }, errors: {}, mate_error: "Mate's settings couldn't be read; these are place-name titles" }),
    })
    const got = await inboxApi.suggestedTitles(['a'])
    expect(got.mateError).toBe("Mate's settings couldn't be read; these are place-name titles")
  })
})
