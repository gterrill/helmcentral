import { renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import { useReplaySigns } from '@/hooks/use-replay-signs'
import { resetQueryStore } from '@/lib/api-query'
import { replaySignsApi } from '@/lib/replay-signs-api'
import { captureOfDocument, ensurePassagePhotosFolder, PASSAGE_PHOTOS_FOLDER, uploadSignPhoto } from '@/lib/sign-photo'
import { jsonResponse, stubApi, type StubRequest } from './helpers/stub-api'

const sign = (id: string, t_ms: number) => ({
  id, t_ms, title: id, note: '', document_id: null, photo_url: null, visibility: 'passing', show_s: 4,
})

beforeEach(() => resetQueryStore())

describe('replay signs', () => {
  it('reads the signs inside the replayed span and refetches after a write', async () => {
    let list = [sign('a', 2000)]
    const mock = stubApi({
      'GET /api/passages/signs?from=1000&to=9000': () => ({ signs: list }),
      'POST /api/passages/signs': (req: StubRequest) => { list = [...list, { ...(req.body as object), id: 'b' } as ReturnType<typeof sign>]; return { sign: list[1] } },
      'PATCH /api/passages/signs/b': (req: StubRequest) => ({ sign: { ...list[1], ...(req.body as object) } }),
      'DELETE /api/passages/signs/b': () => { list = [list[0]]; return { deleted: true } },
    })
    const { result } = renderHook(() => useReplaySigns(1000, 9000))
    await waitFor(() => expect(result.current.signs).toHaveLength(1))
    await replaySignsApi.create({ t_ms: 3000, title: 'Whale', note: '', document_id: null, visibility: 'after', show_s: 4 })
    await waitFor(() => expect(result.current.signs).toHaveLength(2))
    const post = mock.mock.calls.find(([, init]) => init?.method === 'POST')!
    expect(JSON.parse(post[1]!.body as string)).toMatchObject({ title: 'Whale', visibility: 'after', document_id: null })
    await replaySignsApi.patch('b', { document_id: null })
    await replaySignsApi.remove('b')
    await waitFor(() => expect(result.current.signs).toHaveLength(1))
  })

  it('is idle while there is nothing to replay', () => {
    const mock = stubApi({})
    const { result } = renderHook(() => useReplaySigns(null, null))
    expect(result.current.signs).toEqual([])
    expect(mock).not.toHaveBeenCalled()
  })

  it('says why when a write is refused, with the server words', async () => {
    stubApi({ 'POST /api/passages/signs': () => jsonResponse({ error: 'invalid passage request: title is required' }, 400) })
    await expect(replaySignsApi.create({ t_ms: 1, title: '', note: '', document_id: null, visibility: 'passing', show_s: 4 })).rejects.toThrow('title is required')
  })
})

describe('Passage photos folder', () => {
  it('finds the folder at the root by name and does not create another', async () => {
    const mock = stubApi({
      'GET /api/document-folders/tree': { folders: [{ id: 'other', name: 'Manuals', parent_id: null }, { id: 'pp', name: PASSAGE_PHOTOS_FOLDER, parent_id: null }] },
    })
    expect(await ensurePassagePhotosFolder()).toBe('pp')
    expect(mock.mock.calls.some(([, init]) => init?.method === 'POST')).toBe(false)
  })

  it('matches the name without regard to capitals, as the server does, and reads folders only', async () => {
    const mock = stubApi({
      'GET /api/document-folders/tree': { folders: [{ id: 'sub', name: 'Passage photos', parent_id: 'x' }, { id: 'pp', name: 'Passage Photos', parent_id: null }] },
    })
    expect(await ensurePassagePhotosFolder()).toBe('pp')
    expect(mock.mock.calls.map(([u]) => String(u))).toEqual([expect.stringContaining('/api/document-folders/tree')])
  })

  it('creates it on first use', async () => {
    const mock = stubApi({
      'GET /api/document-folders/tree': { folders: [] },
      'POST /api/document-folders': (req: StubRequest) => jsonResponse({ id: 'new', name: (req.body as { name: string }).name }, 201),
    })
    expect(await ensurePassagePhotosFolder()).toBe('new')
    expect(mock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)
  })

  it('two uploads at once make one folder', async () => {
    let created = 0
    stubApi({
      'GET /api/document-folders/tree': { folders: [] },
      'POST /api/document-folders': () => { created += 1; return jsonResponse({ id: 'new', name: PASSAGE_PHOTOS_FOLDER }, 201) },
    })
    const ids = await Promise.all([ensurePassagePhotosFolder(), ensurePassagePhotosFolder()])
    expect(ids).toEqual(['new', 'new'])
    expect(created).toBe(1)
  })

  it('if another tab made it first, uses theirs', async () => {
    let listed = 0
    stubApi({
      'GET /api/document-folders/tree': () => { listed += 1; return { folders: listed === 1 ? [] : [{ id: 'theirs', name: 'passage PHOTOS', parent_id: null }] } },
      'POST /api/document-folders': () => jsonResponse({ error: 'folder name already exists' }, 409),
    })
    expect(await ensurePassagePhotosFolder()).toBe('theirs')
  })

  it('uploads into it', async () => {
    let fields: FormData | null = null
    const mock = stubApi({
      'GET /api/document-folders/tree': { folders: [{ id: 'pp', name: PASSAGE_PHOTOS_FOLDER, parent_id: null }] },
      'POST /api/documents': () => jsonResponse({ document: { id: 'doc1' }, duplicate: false }, 201),
    })
    const id = await uploadSignPhoto(new File([new Uint8Array([1])], 'wave.jpg', { type: 'image/jpeg' }))
    expect(id).toBe('doc1')
    const upload = mock.mock.calls.find(([, init]) => init?.method === 'POST')!
    fields = upload[1]!.body as FormData
    expect(fields.get('folder_id')).toBe('pp')
    expect((fields.get('file') as File).name).toBe('wave.jpg')
  })
})

describe('captureOfDocument', () => {
  it('returns the error when the picture cannot be read, so the sign still goes to the cursor', async () => {
    stubApi({ 'GET /api/documents/d1/content': () => jsonResponse({ error: 'gone' }, 500) })
    const r = await captureOfDocument('d1')
    expect(r).toBeInstanceOf(Error)
    expect((r as Error).message).toMatch(/HTTP 500/)
  })
})
