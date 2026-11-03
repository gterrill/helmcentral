import { useState } from 'react'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'
import { AssistantSection } from '@/components/settings/sections/assistant-section'
import { MateMemoryList } from '@/components/settings/sections/mate-memory-list'
import { initialRegularSettingsDraft, type RegularSettingsDraft } from '@/components/settings/settings-draft'
import { MATE_MEMORIES_PATH } from '@/hooks/use-mate-memories'
import { resetQueryStore } from '@/lib/api-query'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'
import { jsonResponse, stubApi, type StubRequest } from './helpers/stub-api'

// Mate's memory list under Standing notes. The real hook and the real child
// components run against a stubbed server: a click here reaches the request.

// Times on this page are the boat's. The runner is on UTC.
installFakeTelemetryStream({ seeds: { 'vessel-state': { timezone: 'UTC', timezone_source: 'fix' } } })

interface Memory {
  id: string
  text: string
  created_at: string
  updated_at: string
}

const fact = (id: string, text: string): Memory => ({ id, text, created_at: '2026-11-02T03:00:00Z', updated_at: '2026-11-02T03:00:00Z' })

let server: Memory[] = []
let writeStatus = 200

function listBody() {
  return { memories: server, limit: 50, max_chars: 300 }
}

function stubServer() {
  return stubApi({
    [`GET ${MATE_MEMORIES_PATH}`]: () => listBody(),
    [`POST ${MATE_MEMORIES_PATH}`]: (req: StubRequest) => {
      if (writeStatus !== 200) return jsonResponse({ error: 'This session is read-only.' }, writeStatus)
      const created = fact(`m${server.length + 1}`, (req.body as { text: string }).text)
      server = [...server, created]
      return jsonResponse(created, 201)
    },
    [`PATCH ${MATE_MEMORIES_PATH}/m1`]: (req: StubRequest) => {
      if (writeStatus !== 200) return jsonResponse({ error: 'This session is read-only.' }, writeStatus)
      server = server.map((m) => (m.id === 'm1' ? { ...m, text: (req.body as { text: string }).text } : m))
      return server[0]
    },
    [`DELETE ${MATE_MEMORIES_PATH}/m1`]: () => {
      if (writeStatus !== 200) return jsonResponse({ error: 'This session is read-only.' }, writeStatus)
      server = server.filter((m) => m.id !== 'm1')
      return jsonResponse(undefined, 204)
    },
  })
}

beforeEach(() => {
  server = []
  writeStatus = 200
})

afterEach(() => {
  resetQueryStore()
  vi.unstubAllGlobals()
})

describe('Mate memory list', () => {
  it('says so when nothing is kept, and how to keep something', async () => {
    stubServer()
    render(<MateMemoryList />)

    expect(await screen.findByText('No facts kept')).toBeInTheDocument()
    expect(screen.getByText(/Tell Mate "remember that"/)).toBeInTheDocument()
  })

  it('lists each fact with the day it was kept', async () => {
    server = [fact('m1', 'The fuel polisher has no sampling port on its discharge manifold.')]
    stubServer()
    render(<MateMemoryList />)

    expect(await screen.findByText('The fuel polisher has no sampling port on its discharge manifold.')).toBeInTheDocument()
    expect(screen.getByText('Kept 2 Nov 2026')).toBeInTheDocument()
  })

  it('adds a fact by hand', async () => {
    const api = stubServer()
    render(<MateMemoryList />)
    await screen.findByText('No facts kept')

    fireEvent.click(screen.getByRole('button', { name: 'Add fact' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('button', { name: 'Save' })).toBeDisabled()
    fireEvent.change(within(dialog).getByLabelText('Fact'), { target: { value: '  The hatch sticks when wet.  ' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(await screen.findByText('The hatch sticks when wet.')).toBeInTheDocument()
    const post = api.mock.calls.find(([, init]) => init?.method === 'POST')
    expect(JSON.parse(String(post?.[1]?.body))).toEqual({ text: 'The hatch sticks when wet.' })
  })

  it('edits a fact', async () => {
    server = [fact('m1', 'The polisher has a sampling port.')]
    const api = stubServer()
    render(<MateMemoryList />)
    await screen.findByText('The polisher has a sampling port.')

    fireEvent.click(screen.getByRole('button', { name: 'Actions for The polisher has a sampling port.' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog')
    const field = within(dialog).getByLabelText('Fact')
    expect(field).toHaveValue('The polisher has a sampling port.')
    // Unchanged text is not a save.
    expect(within(dialog).getByRole('button', { name: 'Save' })).toBeDisabled()
    fireEvent.change(field, { target: { value: 'The polisher has no sampling port.' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(await screen.findByText('The polisher has no sampling port.')).toBeInTheDocument()
    const patch = api.mock.calls.find(([, init]) => init?.method === 'PATCH')
    expect(JSON.parse(String(patch?.[1]?.body))).toEqual({ text: 'The polisher has no sampling port.' })
  })

  it('removes a fact after asking', async () => {
    server = [fact('m1', 'The polisher has a sampling port.')]
    stubServer()
    render(<MateMemoryList />)
    await screen.findByText('The polisher has a sampling port.')

    fireEvent.click(screen.getByRole('button', { name: 'Actions for The polisher has a sampling port.' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Remove' }))
    const confirm = await screen.findByRole('alertdialog')
    fireEvent.click(within(confirm).getByRole('button', { name: /Remove|Delete/ }))

    await waitFor(() => expect(screen.queryByText('The polisher has a sampling port.')).not.toBeInTheDocument())
    expect(await screen.findByText('No facts kept')).toBeInTheDocument()
  })

  it('a read-only session\'s refused write shows the server\'s reason and keeps the list', async () => {
    server = [fact('m1', 'The polisher has a sampling port.')]
    writeStatus = 403
    stubServer()
    render(<MateMemoryList />)
    await screen.findByText('The polisher has a sampling port.')

    fireEvent.click(screen.getByRole('button', { name: 'Add fact' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Fact'), { target: { value: 'Another fact.' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('read-only')
    expect(screen.getByText('The polisher has a sampling port.')).toBeInTheDocument()
    // The only place the refused text still shows is the open form.
    const shown = screen.queryAllByText('Another fact.')
    expect(shown).toHaveLength(1)
    expect(shown[0].tagName).toBe('TEXTAREA')
  })

  it('stops adding at the limit and says to remove one', async () => {
    server = Array.from({ length: 50 }, (_, i) => fact(`x${i}`, `Fact ${i}`))
    stubServer()
    render(<MateMemoryList />)
    await screen.findByText('Fact 0')

    expect(screen.getByRole('button', { name: 'Add fact' })).toBeDisabled()
    expect(screen.getByText(/The list is full/)).toBeInTheDocument()
  })

  it('shows the list in the Mate section, under Standing notes', async () => {
    server = [fact('m1', 'The polisher has a sampling port.')]
    stubServer()
    function Harness() {
      const [draft, setDraft] = useState<RegularSettingsDraft>({ ...initialRegularSettingsDraft, assistantEnabled: true })
      return (
        <SecretsStatusProvider>
          <AssistantSection draft={draft} onChange={(patch) => setDraft((previous) => ({ ...previous, ...patch }))} />
        </SecretsStatusProvider>
      )
    }
    render(<Harness />)

    const notes = screen.getByLabelText('Mate standing notes')
    const list = await screen.findByText('The polisher has a sampling port.')
    expect(notes.compareDocumentPosition(list) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getAllByText('Facts Mate keeps').length).toBeGreaterThan(0)
  })
})
