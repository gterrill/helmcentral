import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useImportRun } from '@/hooks/use-import-run'
import type { ImportDecisions, ImportDecisionsPatch, ImportRun } from '@/lib/import-run'
import runFixture from './fixtures/yachtwave-run.json'

const fetchImportRun = vi.fn()
const patchImportDecisions = vi.fn()

vi.mock('@/lib/import-run', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/import-run')>()
  return {
    ...actual,
    fetchImportRun: (...args: unknown[]) => fetchImportRun(...args),
    patchImportDecisions: (...args: unknown[]) => patchImportDecisions(...args),
  }
})

function freshRun(): ImportRun {
  return JSON.parse(JSON.stringify(runFixture)) as ImportRun
}

describe('useImportRun', () => {
  let run: ImportRun
  beforeEach(() => {
    run = freshRun()
    fetchImportRun.mockReset().mockImplementation(async () => JSON.parse(JSON.stringify(run)))
    patchImportDecisions.mockReset()
  })

  it('keeps an unsent attach-to-equipment choice when a file upload comes back', async () => {
    run.decisions.files['file-a'] = { document_id: '', skipped: false, equipment_key: '' }
    const { result } = renderHook(() => useImportRun('run-1'))
    await waitFor(() => expect(result.current.decisions).not.toBeNull())

    act(() => result.current.setDecision('files', 'file-a', { document_id: '', skipped: false, equipment_key: 'eq-1' }))

    const uploaded = freshRun()
    uploaded.decisions.files['file-a'] = { document_id: 'doc-9', skipped: false, equipment_key: '' }
    act(() => result.current.adoptUpload(uploaded, 'file-a'))

    expect(result.current.decisions?.files['file-a']).toEqual({ document_id: 'doc-9', skipped: false, equipment_key: 'eq-1' })

    patchImportDecisions.mockImplementation(async () => uploaded)
    await act(async () => { await result.current.flush() })
    const sent = patchImportDecisions.mock.calls[0][1] as ImportDecisionsPatch
    expect(sent.files?.['file-a'].equipment_key).toBe('eq-1')
  })

  it('does not revert an edit made while a save is in flight, and sends it next', async () => {
    const { result } = renderHook(() => useImportRun('run-1'))
    await waitFor(() => expect(result.current.decisions).not.toBeNull())
    const key = Object.keys(result.current.decisions!.records)[0]
    const first: ImportDecisions['records'][string] = { action: 'skip', target_id: '' }
    const second: ImportDecisions['records'][string] = { action: 'create', target_id: '' }

    act(() => result.current.setDecision('records', key, first))
    let release: (r: ImportRun) => void = () => {}
    patchImportDecisions.mockImplementationOnce(() => new Promise<ImportRun>((resolve) => { release = resolve }))
    let flushing: Promise<void> = Promise.resolve()
    act(() => { flushing = result.current.flush() })

    act(() => result.current.setDecision('records', key, second))
    const saved = freshRun()
    saved.decisions.records[key] = first
    await act(async () => { release(saved); await flushing })

    expect(result.current.decisions?.records[key]).toEqual(second)

    patchImportDecisions.mockImplementationOnce(async () => saved)
    await act(async () => { await result.current.flush() })
    const sent = patchImportDecisions.mock.calls[1][1] as ImportDecisionsPatch
    expect(sent.records?.[key]).toEqual(second)
  })
})
