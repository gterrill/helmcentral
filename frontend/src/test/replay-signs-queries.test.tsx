import { renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useReplaySigns } from '@/hooks/use-replay-signs'
import { resetQueryStore } from '@/lib/api-query'
import { INBOX_MAX_DIMENSION } from '@/lib/inbox-image'
import { replaySignsApi } from '@/lib/replay-signs-api'
import { captureOfDocument, ensurePassagePhotosFolder, isSignPhotoType, isSignUploadFile, PASSAGE_PHOTOS_FOLDER, SIGN_UPLOAD_ACCEPT, uploadSignPhoto } from '@/lib/sign-photo'
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
  // The box finds or makes the folder (one rule, the one the Inbox uses); the
  // browser no longer reads the folder tree or creates folders itself.
  it('asks the box for the folder and uses the id it gives', async () => {
    const mock = stubApi({ 'POST /api/passage-photos-folder': { id: 'pp', name: PASSAGE_PHOTOS_FOLDER } })
    expect(await ensurePassagePhotosFolder()).toBe('pp')
    expect(mock.mock.calls.map(([u, init]) => `${init?.method} ${new URL(String(u), 'http://x').pathname}`)).toEqual(['POST /api/passage-photos-folder'])
  })

  it('says why when the box cannot give the folder', async () => {
    stubApi({ 'POST /api/passage-photos-folder': () => jsonResponse({ error: 'the document library is not running' }, 503) })
    await expect(ensurePassagePhotosFolder()).rejects.toThrow('the document library is not running')
  })

  it('uploads the shrunk copy into it, as a JPEG', async () => {
    const mock = stubApi({
      'POST /api/passage-photos-folder': { id: 'pp', name: PASSAGE_PHOTOS_FOLDER },
      'POST /api/documents': () => jsonResponse({ document: { id: 'doc1' }, duplicate: false }, 201),
    })
    const shrink = vi.fn(async () => new Blob(['small'], { type: 'image/jpeg' }))
    const original = new File([new Uint8Array([1])], 'wave.HEIC', { type: 'image/heic' })
    const id = await uploadSignPhoto(original, shrink)
    expect(id).toBe('doc1')
    // The same size and quality as the Inbox's: its long edge and the encoder's default.
    expect(shrink).toHaveBeenCalledWith(original, INBOX_MAX_DIMENSION, expect.objectContaining({ reduceDecode: true }))
    expect(INBOX_MAX_DIMENSION).toBe(2560)
    const upload = mock.mock.calls.find(([u]) => String(u).endsWith('/api/documents'))!
    const fields = upload[1]!.body as FormData
    expect(fields.get('folder_id')).toBe('pp')
    const sent = fields.get('file') as File
    expect(sent.name).toBe('wave.jpg')
    expect(sent.type).toBe('image/jpeg')
    expect(await sent.text()).toBe('small')
  })

  it('uploads nothing when the photo cannot be shrunk, rather than sending the original', async () => {
    const mock = stubApi({ 'POST /api/passage-photos-folder': { id: 'pp', name: PASSAGE_PHOTOS_FOLDER } })
    const shrink = vi.fn(async () => { throw new Error('could not decode the photo') })
    await expect(uploadSignPhoto(new File([new Uint8Array([1])], 'x.jpg', { type: 'image/jpeg' }), shrink)).rejects.toThrow('could not decode the photo')
    expect(mock.mock.calls.some(([u]) => String(u).endsWith('/api/documents'))).toBe(false)
  })
})

describe('what the sign editor takes from a phone', () => {
  const file = (name: string, type: string) => new File([new Uint8Array([1])], name, { type })

  it('accepts HEIC and HEIF as well as the usual pictures, by type or by name', () => {
    for (const f of [file('a.jpg', 'image/jpeg'), file('a.png', 'image/png'), file('a.gif', 'image/gif'), file('a.webp', 'image/webp'),
      file('a.heic', 'image/heic'), file('a.heif', 'image/heif'), file('IMG_1.HEIC', ''), file('IMG_2.heif', '')]) {
      expect(isSignUploadFile(f), f.name).toBe(true)
    }
    expect(isSignUploadFile(file('r.pdf', 'application/pdf'))).toBe(false)
    expect(SIGN_UPLOAD_ACCEPT).toEqual(expect.stringContaining('image/heic'))
    expect(SIGN_UPLOAD_ACCEPT).toEqual(expect.stringContaining('.heif'))
  })

  it('still takes only the types a library photo can be, from the library', () => {
    expect(isSignPhotoType('image/heic')).toBe(false)
    expect(isSignPhotoType('image/jpeg')).toBe(true)
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
