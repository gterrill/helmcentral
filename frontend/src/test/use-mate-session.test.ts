import { describe, it, expect, afterEach, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

import { useMateSession, type MateSendResult } from '@/hooks/use-mate-session'
import type { AssistantScreenContext } from '@/hooks/use-assistant-chat'
import { todayISO } from '@/lib/local-date'
import { removeMateWatch } from '@/lib/mate-watch-store'
import { controllableSseStream, flushMicrotasks, sseFrame, sseStream } from './helpers/fake-sse'

// One Mate session is everything a Mate surface (the panel, the sheet) does
// with a conversation: which one is open, its messages, asking a question
// and reading the answer back, rejoining an answer still being written, and
// stopping one. These tests drive it against a fake server.

interface FetchLike {
  ok: boolean
  status?: number
  json?: () => Promise<unknown>
  body?: ReadableStream<Uint8Array> | null
}

interface ConversationRecord {
  id: string
  title: string
  created_at: string
  updated_at: string
}

type MessageRecord = Record<string, unknown> & { id: string; role: string; content: string }

function conversation(id: string, title = id): ConversationRecord {
  return { id, title, created_at: '2026-10-15T00:00:00Z', updated_at: '2026-10-15T00:00:00Z' }
}

function fakeMateServer(initial: { conversations?: ConversationRecord[]; messages?: Record<string, MessageRecord[]> } = {}) {
  const conversations = [...(initial.conversations ?? [])]
  const messages = new Map<string, MessageRecord[]>(Object.entries(initial.messages ?? {}))
  const posts: Array<{ conversationId: string; body: Record<string, unknown> }> = []
  const cancels: string[] = []
  const runs: string[] = []
  const ratings: Array<{ method: string; messageId: string; body?: Record<string, unknown> }> = []
  let counter = 0

  const store = (conversationId: string, record: { id: string; role: string; content: string }): MessageRecord => {
    const list = messages.get(conversationId) ?? []
    const full: MessageRecord = { ...record, conversation_id: conversationId, seq: list.length + 1, created_at: '2026-10-15T00:00:00Z' }
    messages.set(conversationId, [...list, full])
    return full
  }

  // Stores the assistant's reply and returns the SSE body that delivers it.
  const answer = (conversationId: string, content: string): FetchLike => {
    counter += 1
    const message = store(conversationId, { id: `a${counter}`, role: 'assistant', content })
    const convo = conversations.find((c) => c.id === conversationId) ?? conversation(conversationId)
    return { ok: true, body: sseStream([sseFrame('message', { message, conversation: convo })]) }
  }

  const server = {
    posts,
    cancels,
    runs,
    ratings,
    messages,
    store,
    answer,
    onSend: ((conversationId: string) => answer(conversationId, 'Aye.')) as (conversationId: string, body: Record<string, unknown>) => FetchLike | Promise<FetchLike>,
    onRun: ((): FetchLike => ({ ok: true, status: 204 })) as (conversationId: string) => FetchLike | Promise<FetchLike>,
    /** Awaited before a conversation read answers; tests hold it to keep a read in flight. */
    readGate: null as Promise<void> | null,
    /** Every request, as 'METHOD path', in order. */
    log: [] as string[],
    fetch: vi.fn(async (url: string, init?: RequestInit): Promise<FetchLike> => {
      const method = init?.method ?? 'GET'
      server.log.push(`${method} ${url.replace(/^.*\/api\//, '/api/')}`)
      if (url.endsWith('/api/assistant/conversations') && method === 'GET') {
        return { ok: true, json: async () => ({ conversations }) }
      }
      if (url.endsWith('/api/assistant/conversations') && method === 'POST') {
        counter += 1
        const created = conversation(`new-${counter}`, 'New conversation')
        conversations.unshift(created)
        messages.set(created.id, [])
        return { ok: true, status: 201, json: async () => created }
      }
      const send = url.match(/\/api\/assistant\/conversations\/([^/]+)\/messages$/)
      if (send && method === 'POST') {
        const id = decodeURIComponent(send[1])
        const body = JSON.parse(init?.body as string) as Record<string, unknown>
        posts.push({ conversationId: id, body })
        counter += 1
        store(id, { id: `u${counter}`, role: 'user', content: body.content as string })
        return server.onSend(id, body)
      }
      const cancel = url.match(/\/api\/assistant\/conversations\/([^/]+)\/run\/cancel$/)
      if (cancel && method === 'POST') {
        cancels.push(decodeURIComponent(cancel[1]))
        return { ok: true, status: 204 }
      }
      const run = url.match(/\/api\/assistant\/conversations\/([^/]+)\/run$/)
      if (run && method === 'GET') {
        const id = decodeURIComponent(run[1])
        runs.push(id)
        return server.onRun(id)
      }
      const read = url.match(/\/api\/assistant\/conversations\/([^/]+)$/)
      if (read && method === 'GET') {
        const id = decodeURIComponent(read[1])
        const convo = conversations.find((c) => c.id === id) ?? conversation(id)
        if (server.readGate) await server.readGate
        const list = messages.get(id) ?? []
        return { ok: true, json: async () => ({ conversation: convo, messages: list }) }
      }
      const rating = url.match(/\/api\/assistant\/messages\/([^/]+)\/feedback$/)
      if (rating && (method === 'PUT' || method === 'DELETE')) {
        const messageId = decodeURIComponent(rating[1])
        const body = init?.body ? (JSON.parse(init.body as string) as Record<string, unknown>) : undefined
        ratings.push({ method, messageId, body })
        if (method === 'DELETE') return { ok: true, status: 204 }
        return { ok: true, json: async () => ({ feedback: { message_id: messageId, ...body } }) }
      }
      throw new Error(`Unhandled fetch in test: ${method} ${url}`)
    }),
  }
  vi.stubGlobal('fetch', server.fetch)
  return server
}

async function renderSession(options: Parameters<typeof useMateSession>[0] = {}) {
  const hook = renderHook((props: Parameters<typeof useMateSession>[0]) => useMateSession(props), { initialProps: options })
  await waitFor(() => expect(hook.result.current.loading).toBe(false))
  return hook
}

describe('useMateSession', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  describe('send', () => {
    it('creates the conversation on the first question and opens it', async () => {
      const server = fakeMateServer()
      const { result } = await renderSession()
      expect(result.current.activeId).toBeNull()

      let outcome!: MateSendResult
      await act(async () => {
        outcome = await result.current.send('Anchorage for tonight?')
      })

      expect(result.current.activeId).toBe('new-1')
      expect(server.posts).toEqual([
        { conversationId: 'new-1', body: expect.objectContaining({ content: 'Anchorage for tonight?', today: todayISO() }) },
      ])
      expect(outcome.answered).toBe(true)
      expect(outcome.message?.content).toBe('Aye.')
      removeMateWatch('new-1')
    })

    it('sends a first question with nothing but the create and the question, no read or rejoin before it', async () => {
      const server = fakeMateServer()
      const { result } = await renderSession()
      server.log.length = 0

      await act(async () => {
        await result.current.send('Anchorage for tonight?')
      })

      // The list is re-read for the new title; the conversation itself is not.
      const beforeAnswer = server.log
        .slice(0, server.log.indexOf('POST /api/assistant/conversations/new-1/messages') + 1)
        .filter((line) => line !== 'GET /api/assistant/conversations')
      expect(beforeAnswer).toEqual(['POST /api/assistant/conversations', 'POST /api/assistant/conversations/new-1/messages'])
      expect(server.runs).toEqual([])
      expect(result.current.activeId).toBe('new-1')
      removeMateWatch('new-1')
    })

    it('takes a follow-up as soon as the answer has landed, while the thread is still being re-read', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))
      let openGate!: () => void
      server.readGate = new Promise((resolve) => { openGate = resolve })

      let first!: MateSendResult
      await act(async () => {
        first = await result.current.send('Is the tide turning?')
      })
      expect(first.answered).toBe(true)

      let second!: MateSendResult
      await act(async () => {
        const pending = result.current.send('And the wind?')
        openGate()
        second = await pending
      })

      expect(second.refused).toBeUndefined()
      expect(server.posts).toHaveLength(2)
      expect(result.current.status.error).toBeNull()
      removeMateWatch('c1')
    })

    it('sends into the open conversation without creating another', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      await act(async () => {
        await result.current.send('And tomorrow?')
      })

      expect(server.posts.map((p) => p.conversationId)).toEqual(['c1'])
      const creates = server.fetch.mock.calls.filter(([url, init]) => url.endsWith('/api/assistant/conversations') && init?.method === 'POST')
      expect(creates).toHaveLength(0)
    })

    it('posts only the question and the date when there is no screen, voice or attachment', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      await act(async () => {
        await result.current.send('Plain question', { attachments: [] })
      })

      expect(server.posts[0].body).toEqual({ content: 'Plain question', today: todayISO() })
    })

    it('shows the question at once, before the server has answered', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const stream = controllableSseStream()
      server.onSend = () => ({ ok: true, body: stream.stream })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      let pending!: Promise<MateSendResult>
      await act(async () => {
        pending = result.current.send('Is the tide turning?', { attachments: [{ documentId: 'd1', filename: 'tides.pdf' }] })
        await flushMicrotasks()
      })

      expect(result.current.messages).toEqual([
        expect.objectContaining({ role: 'user', content: 'Is the tide turning?', attachments: [{ documentId: 'd1', filename: 'tides.pdf' }] }),
      ])
      expect(result.current.messages[0].id).toMatch(/^local-/)
      expect(result.current.status.sending).toBe(true)
      expect(server.posts[0].body.attachments).toEqual(['d1'])

      await act(async () => {
        stream.close()
        await pending
      })
    })

    it('swaps the optimistic question for the server copy once the answer lands, without a loading flash', async () => {
      fakeMateServer({ conversations: [conversation('c1')] })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      const loadingSeen: boolean[] = []
      await act(async () => {
        await result.current.send('Is the tide turning?')
      })
      loadingSeen.push(result.current.conversationLoading)

      await waitFor(() => expect(result.current.messages.map((m) => m.id)).toEqual(['u1', 'a2']))
      expect(result.current.messages[0]).toEqual(expect.objectContaining({ role: 'user', content: 'Is the tide turning?', seq: 1 }))
      expect(loadingSeen).toEqual([false])
    })

    it('does not put a reply into a conversation the operator has left', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const stream = controllableSseStream()
      server.onSend = () => ({ ok: true, body: stream.stream })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      let pending!: Promise<MateSendResult>
      await act(async () => {
        pending = result.current.send('Is the tide turning?')
        await flushMicrotasks()
      })
      act(() => result.current.startNew())

      let outcome!: MateSendResult
      await act(async () => {
        const message = server.store('c1', { id: 'a9', role: 'assistant', content: 'Turning at 14:10.' })
        stream.push(sseFrame('message', { message, conversation: conversation('c1') }))
        stream.close()
        outcome = await pending
      })

      expect(outcome).toEqual({ answered: true, message: null })
      expect(result.current.activeId).toBeNull()
      expect(result.current.messages).toEqual([])
    })

    it('lets go of the stream when the operator opens another conversation mid-answer, and rejoins that one', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1'), conversation('c2')] })
      let cancelled = false
      server.onSend = () => ({
        ok: true,
        body: new ReadableStream({ start() {}, cancel() { cancelled = true } }),
      })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      let pending!: Promise<MateSendResult>
      await act(async () => {
        pending = result.current.send('Is the tide turning?')
        await flushMicrotasks()
      })
      await act(async () => {
        await result.current.select('c2')
      })

      await waitFor(() => expect(server.runs).toContain('c2'))
      const outcome = await pending
      expect(outcome.answered).toBe(false)
      expect(cancelled).toBe(true)
      // The run itself goes on: only Stop cancels it.
      expect(server.cancels).toEqual([])
      expect(result.current.status.sending).toBe(false)
      removeMateWatch('c1')
    })

    it('refuses a second question while the first is still being answered, and says so', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const stream = controllableSseStream()
      server.onSend = () => ({ ok: true, body: stream.stream })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      let first!: Promise<MateSendResult>
      await act(async () => {
        first = result.current.send('Is the tide turning?')
        await flushMicrotasks()
      })
      let second!: MateSendResult
      await act(async () => {
        second = await result.current.send('And the wind?')
      })

      expect(second).toEqual({ answered: false, message: null, refused: 'busy' })
      expect(server.posts).toHaveLength(1)
      expect(result.current.status.error).toBe('Mate is still answering the last question.')
      expect(result.current.messages.map((m) => m.content)).toEqual(['Is the tide turning?'])

      await act(async () => {
        stream.close()
        await first
      })
      removeMateWatch('c1')
    })

    it('drops the still-answering message once that answer lands', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const stream = controllableSseStream()
      server.onSend = () => ({ ok: true, body: stream.stream })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      let first!: Promise<MateSendResult>
      await act(async () => {
        first = result.current.send('Is the tide turning?')
        await flushMicrotasks()
      })
      await act(async () => {
        await result.current.send('And the wind?')
      })
      expect(result.current.status.error).toBe('Mate is still answering the last question.')

      await act(async () => {
        const message = server.store('c1', { id: 'a9', role: 'assistant', content: 'Turning at 14:10.' })
        stream.push(sseFrame('message', { message, conversation: conversation('c1') }))
        stream.close()
        await first
      })

      expect(result.current.status.error).toBeNull()
      removeMateWatch('c1')
    })

    it('refuses a question sent in the same tick as another, before any re-render', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const stream = controllableSseStream()
      server.onSend = () => ({ ok: true, body: stream.stream })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      let outcomes!: Promise<MateSendResult[]>
      await act(async () => {
        const send = result.current.send
        outcomes = Promise.all([send('One'), send('Two')])
        await flushMicrotasks()
      })
      await act(async () => {
        stream.close()
        await outcomes
      })

      expect((await outcomes)[1].refused).toBe('busy')
      expect(server.posts).toHaveLength(1)
      removeMateWatch('c1')
    })

    it('sends a new-conversation question to a fresh conversation even while another answer streams', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const stream = controllableSseStream()
      server.onSend = (id) => (id === 'c1' ? { ok: true, body: stream.stream } : server.answer(id, 'Fresh answer.'))
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      await act(async () => {
        void result.current.send('Is the tide turning?')
        await flushMicrotasks()
      })
      let outcome!: MateSendResult
      await act(async () => {
        outcome = await result.current.send('Why did the logs fail?', { newConversation: true })
      })

      expect(server.posts.map((p) => p.conversationId)).toEqual(['c1', 'new-2'])
      expect(outcome.message?.content).toBe('Fresh answer.')
      expect(result.current.activeId).toBe('new-2')
      // The first run is not cancelled; it finishes server side.
      expect(server.cancels).toEqual([])
      removeMateWatch('c1')
      removeMateWatch('new-2')
    })

    it('reports the server refusing a question as Mate still answering', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      server.onSend = () => ({ ok: false, status: 409, json: async () => ({ error: 'the assistant is still answering the previous message' }) })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      let outcome!: MateSendResult
      await act(async () => {
        outcome = await result.current.send('And the wind?')
      })

      expect(outcome).toEqual({ answered: false, message: null })
      expect(result.current.status.error).toBe('Mate is still answering the last question.')
    })
  })

  describe('screen and voice', () => {
    it('sends the screen the operator is on with every question, read at the moment it is sent', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      let screen: AssistantScreenContext = { panel: 'forecast' }
      const { result } = await renderSession({ initialId: 'c1', screen: () => screen })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      await act(async () => {
        await result.current.send('How does tomorrow look?')
      })
      screen = { panel: 'documents', document_id: 'd7', document_title: 'Watermaker manual' }
      await act(async () => {
        await result.current.send('What does this say about the membrane?')
      })

      expect(server.posts.map((p) => p.body.screen)).toEqual([
        { panel: 'forecast' },
        { panel: 'documents', document_id: 'd7', document_title: 'Watermaker manual' },
      ])
    })

    it('accepts the screen as a plain value, following the latest render', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const { result, rerender } = await renderSession({ initialId: 'c1', screen: { panel: 'forecast' } })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))
      rerender({ initialId: 'c1', screen: { panel: 'settings', section: 'assistant' } })

      await act(async () => {
        await result.current.send('Which model is this?')
      })

      expect(server.posts[0].body.screen).toEqual({ panel: 'settings', section: 'assistant' })
    })

    it('asks for a spoken answer only for a question that came in by voice', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      await act(async () => {
        await result.current.send('typed question')
      })
      await act(async () => {
        await result.current.send('spoken question', { spoken: true })
      })

      expect(server.posts[0].body).not.toHaveProperty('spoken')
      expect(server.posts[1].body.spoken).toBe(true)
    })
  })

  describe('rejoining an answer still being written', () => {
    it('rejoins the run of a conversation the operator opens', async () => {
      const server = fakeMateServer({
        conversations: [conversation('c1')],
        messages: { c1: [{ id: 'u1', role: 'user', content: 'Is the tide turning?', seq: 1, conversation_id: 'c1', created_at: '' }] },
      })
      server.onRun = (id) => {
        const message = server.store(id, { id: 'a2', role: 'assistant', content: 'Turning at 14:10.' })
        return { ok: true, body: sseStream([sseFrame('message', { message, conversation: conversation(id) })]) }
      }
      const { result } = await renderSession()

      await act(async () => {
        await result.current.select('c1')
      })

      await waitFor(() => expect(result.current.messages.map((m) => m.content)).toEqual(['Is the tide turning?', 'Turning at 14:10.']))
      expect(server.runs).toEqual(['c1'])
    })

    it('looks for no running answer while no conversation is open', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      await renderSession()
      await act(async () => { await flushMicrotasks() })

      expect(server.runs).toEqual([])
    })

    it('rejoin() picks up a run started server side and reloads the thread so its report row shows', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(server.runs).toEqual(['c1']))

      // A watch ended: the server wrote its report row and started Mate's turn.
      server.store('c1', { id: 'w1', role: 'watch', content: 'Watch finished: Port engine load (5 min)\nreadings' })
      server.onRun = (id) => {
        const message = server.store(id, { id: 'a2', role: 'assistant', content: 'Load held at 60 %.' })
        return { ok: true, body: sseStream([sseFrame('message', { message, conversation: conversation(id) })]) }
      }
      await act(async () => {
        await result.current.rejoin()
      })

      await waitFor(() => expect(result.current.messages.map((m) => m.id)).toEqual(['w1', 'a2']))
      expect(server.runs).toEqual(['c1', 'c1'])
    })

    it('rejoin() leaves a conversation alone while its own answer is still streaming', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const stream = controllableSseStream()
      server.onSend = () => ({ ok: true, body: stream.stream })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(server.runs).toEqual(['c1']))

      let pending!: Promise<MateSendResult>
      await act(async () => {
        pending = result.current.send('Is the tide turning?')
        await flushMicrotasks()
      })
      expect(result.current.isAnswering()).toBe(true)
      await act(async () => {
        await result.current.rejoin()
      })

      expect(server.runs).toEqual(['c1'])
      expect(result.current.status.sending).toBe(true)
      await act(async () => {
        stream.close()
        await pending
      })
      removeMateWatch('c1')
    })
  })

  describe('stop', () => {
    it('cancels the run, says it stopped, and clears that on the next question', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const stream = controllableSseStream()
      server.onSend = () => ({ ok: true, body: stream.stream })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      await act(async () => {
        void result.current.send('Is the tide turning?')
        await flushMicrotasks()
      })
      await act(async () => {
        result.current.stop()
        await flushMicrotasks()
      })

      expect(server.cancels).toEqual(['c1'])
      await waitFor(() => expect(result.current.status.sending).toBe(false))
      expect(result.current.status.stopped).toBe(true)

      server.onSend = (id) => server.answer(id, 'Aye.')
      await act(async () => {
        await result.current.send('Try again')
      })
      expect(result.current.status.stopped).toBe(false)
      removeMateWatch('c1')
    })

    it('says Stopped. for a rejoined answer cut off before it arrived', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const stream = controllableSseStream()
      server.onRun = () => ({ ok: true, body: stream.stream })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.status.sending).toBe(true))

      await act(async () => {
        result.current.stop()
        await flushMicrotasks()
      })

      expect(server.cancels).toEqual(['c1'])
      expect(result.current.status.stopped).toBe(true)
    })

    it('says Stopped. only under the conversation it happened in', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1'), conversation('c2')] })
      const stream = controllableSseStream()
      server.onSend = () => ({ ok: true, body: stream.stream })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      await act(async () => {
        void result.current.send('Is the tide turning?')
        await flushMicrotasks()
      })
      await act(async () => {
        result.current.stop()
        await flushMicrotasks()
      })
      await waitFor(() => expect(result.current.status.stopped).toBe(true))

      await act(async () => {
        await result.current.select('c2')
      })
      expect(result.current.status.stopped).toBe(false)
      removeMateWatch('c1')
    })

    it('does not call an answer that had already arrived stopped', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const stream = controllableSseStream()
      server.onSend = () => ({ ok: true, body: stream.stream })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      await act(async () => {
        void result.current.send('Is the tide turning?')
        await flushMicrotasks()
      })
      await act(async () => {
        const message = server.store('c1', { id: 'a9', role: 'assistant', content: 'Turning at 14:10.' })
        stream.push(sseFrame('message', { message, conversation: conversation('c1') }))
        await flushMicrotasks()
      })
      await act(async () => {
        result.current.stop()
        await flushMicrotasks()
      })

      expect(result.current.status.stopped).toBe(false)
      expect(result.current.messages.map((m) => m.content)).toContain('Turning at 14:10.')
      removeMateWatch('c1')
    })
  })

  describe('rating answers', () => {
    it('offers the rating on an answer delivered live, not on one read back', async () => {
      const server = fakeMateServer({
        conversations: [conversation('c1')],
        messages: { c1: [{ id: 'old', role: 'assistant', content: 'Earlier answer' }] },
      })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.messages).toHaveLength(1))
      expect(result.current.liveAnswerIds.has('old')).toBe(false)

      await act(async () => {
        await result.current.send('Is the tide turning?')
      })
      const answer = server.messages.get('c1')!.at(-1)!
      expect(result.current.liveAnswerIds.has(answer.id)).toBe(true)
      expect(result.current.liveAnswerIds.has('old')).toBe(false)
      removeMateWatch('c1')
    })

    it('forgets which answers were live once another conversation is opened', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1'), conversation('c2')] })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))
      await act(async () => {
        await result.current.send('Is the tide turning?')
      })
      const answer = server.messages.get('c1')!.at(-1)!
      expect(result.current.liveAnswerIds.has(answer.id)).toBe(true)

      await act(async () => {
        await result.current.select('c2')
      })
      await act(async () => {
        await result.current.select('c1')
      })
      expect(result.current.liveAnswerIds.has(answer.id)).toBe(false)
      removeMateWatch('c1')
    })

    it('saves a verdict with its note, replaces it, and clears it', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))

      await act(async () => {
        await result.current.rateAnswer('a1', 'down', '')
      })
      expect(result.current.ratings.a1).toEqual({ verdict: 'down', note: '' })
      await act(async () => {
        await result.current.rateAnswer('a1', 'down', 'wrong tank')
      })
      expect(result.current.ratings.a1).toEqual({ verdict: 'down', note: 'wrong tank' })
      await act(async () => {
        await result.current.rateAnswer('a1', null)
      })
      expect(result.current.ratings.a1).toBeUndefined()

      expect(server.ratings).toEqual([
        { method: 'PUT', messageId: 'a1', body: { verdict: 'down', note: '' } },
        { method: 'PUT', messageId: 'a1', body: { verdict: 'down', note: 'wrong tank' } },
        { method: 'DELETE', messageId: 'a1', body: undefined },
      ])
      removeMateWatch('c1')
    })

    it('keeps the old verdict and says so when the server refuses the new one', async () => {
      const server = fakeMateServer({ conversations: [conversation('c1')] })
      const { result } = await renderSession({ initialId: 'c1' })
      await waitFor(() => expect(result.current.activeId).toBe('c1'))
      await act(async () => {
        await result.current.rateAnswer('a1', 'up', '')
      })
      const original = server.fetch.getMockImplementation()!
      server.fetch.mockImplementation(async (url: string, init?: RequestInit) =>
        init?.method === 'PUT' ? { ok: false, status: 404, json: async () => ({ error: 'message not found' }) } : original(url, init))

      let failure: string | null = null
      await act(async () => {
        failure = await result.current.rateAnswer('a1', 'down', '')
      })
      expect(failure).toBeTruthy()
      expect(result.current.ratings.a1?.verdict).toBe('up')
    })
  })
})
