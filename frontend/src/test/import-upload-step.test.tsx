import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { ImportUploadStep } from '@/components/import/import-upload-step'
import { ImportSection } from '@/components/settings/sections/import-section'
import runFixture from './fixtures/yachtwave-run.json'

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
  globalThis.localStorage?.clear()
})

function exportFile(name = 'Export_Pikorua.html') {
  return new File(['<html></html>'], name, { type: 'text/html' })
}

describe('ImportUploadStep', () => {
  it('cannot read an export until a file is chosen', () => {
    render(<ImportUploadStep onCreated={vi.fn()} />)
    expect(screen.getByRole('button', { name: 'Read export' })).toBeDisabled()
  })

  it('posts the chosen file as a YachtWave export and hands over the run', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 201, json: async () => runFixture })
    const onCreated = vi.fn()
    render(<ImportUploadStep onCreated={onCreated} />)

    fireEvent.change(screen.getByLabelText('YachtWave export file'), { target: { files: [exportFile()] } })
    fireEvent.click(screen.getByRole('button', { name: 'Read export' }))

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(expect.objectContaining({ id: 'run-1' })))
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/import/runs')
    const form = (init as RequestInit).body as FormData
    expect(form.get('source')).toBe('yachtwave')
    expect((form.get('file') as File).name).toBe('Export_Pikorua.html')
  })

  it('accepts a dropped file', async () => {
    fetchMock.mockResolvedValue({ ok: true, status: 201, json: async () => runFixture })
    render(<ImportUploadStep onCreated={vi.fn()} />)

    fireEvent.drop(screen.getByText(/Drop the export file here/).parentElement!, { dataTransfer: { files: [exportFile('dropped.html')] } })

    expect(await screen.findByText('dropped.html')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Read export' })).toBeEnabled()
  })

  it('shows the backend rejection word for word', async () => {
    const message = 'this file is not a YachtWave Vessel Export (expected the Vessel Export page saved from YachtWave)'
    fetchMock.mockResolvedValue({ ok: false, status: 400, json: async () => ({ error: message }) })
    const onCreated = vi.fn()
    render(<ImportUploadStep onCreated={onCreated} />)

    fireEvent.change(screen.getByLabelText('YachtWave export file'), { target: { files: [exportFile('notes.html')] } })
    fireEvent.click(screen.getByRole('button', { name: 'Read export' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(message)
    expect(onCreated).not.toHaveBeenCalled()
  })
})

describe('ImportSection', () => {
  it('lists YachtWave and opens the upload page from its button', () => {
    const onOpenImport = vi.fn()
    render(<ImportSection onOpenImport={onOpenImport} />)

    expect(screen.getByText('YachtWave')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Import from YachtWave' }))

    expect(onOpenImport).toHaveBeenCalledWith('new')
  })

  it('offers to resume the draft this browser last worked on', async () => {
    globalThis.localStorage?.setItem('helmcentral.import.draft-run', 'run-1')
    fetchMock.mockResolvedValue({ ok: true, status: 200, json: async () => runFixture })
    const onOpenImport = vi.fn()
    render(<ImportSection onOpenImport={onOpenImport} />)

    fireEvent.click(await screen.findByRole('button', { name: 'Resume' }))

    expect(onOpenImport).toHaveBeenCalledWith('run-1')
  })

  it('offers no resume for a draft that has since been committed', async () => {
    globalThis.localStorage?.setItem('helmcentral.import.draft-run', 'run-1')
    fetchMock.mockResolvedValue({ ok: true, status: 200, json: async () => ({ ...runFixture, status: 'committed' }) })
    render(<ImportSection onOpenImport={vi.fn()} />)

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    await waitFor(() => expect(globalThis.localStorage?.getItem('helmcentral.import.draft-run')).toBeNull())
    expect(screen.queryByRole('button', { name: 'Resume' })).not.toBeInTheDocument()
  })

  it('shows a server error about the draft instead of hiding it', async () => {
    globalThis.localStorage?.setItem('helmcentral.import.draft-run', 'run-1')
    fetchMock.mockResolvedValue({ ok: false, status: 500, json: async () => ({ error: 'the import store is unavailable' }) })
    render(<ImportSection onOpenImport={vi.fn()} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('the import store is unavailable')
  })
})
