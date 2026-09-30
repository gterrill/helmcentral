import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'

import { ImportConfirmStep } from '@/components/import/import-confirm-step'
import { ImportWizard } from '@/components/import/import-wizard'
import type { ImportDecisions, ImportRun } from '@/lib/import-run'
import runFixture from './fixtures/yachtwave-run.json'

// The fixture is a trimmed copy of a real staged run: the redacted
// Pikorua export, parsed by the backend (backend/testdata/yachtwave), so every
// key, decision and issue has the shape the server actually sends.

vi.mock('@/hooks/use-vessel-identity', () => ({ useVesselIdentity: () => ({ boatName: 'M/V Pikorua' }) }))
vi.mock('@/hooks/use-vessel-state', () => ({ useVesselState: () => ({ vesselLengthOverallM: 17.9, vesselDraftM: 1.5 }) }))

let run: ImportRun
const fetchMock = vi.fn()
let patchBodies: Array<{ decisions: Partial<ImportDecisions> }>
let patchFailure: string | null
let commitResponse: { status: number; body: unknown }

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

function freshRun(): ImportRun {
  return clone(runFixture) as unknown as ImportRun
}

function merge(target: ImportDecisions, patch: Partial<ImportDecisions>) {
  for (const category of Object.keys(patch) as Array<keyof ImportDecisions>) {
    Object.assign(target[category], patch[category])
  }
}

function ok(body: unknown, status = 200) {
  return Promise.resolve({ ok: status < 400, status, json: async () => body })
}

function stubFetch() {
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'

    if (u === '/api/import/runs/run-1' && method === 'GET') return ok(run)
    if (u === '/api/import/runs/run-1' && method === 'PATCH') {
      const body = JSON.parse(String(init?.body)) as { decisions: Partial<ImportDecisions> }
      patchBodies.push(body)
      if (patchFailure !== null) return ok({ error: patchFailure }, 400)
      merge(run.decisions, body.decisions)
      return ok(run)
    }
    if (u === '/api/import/runs/run-1' && method === 'DELETE') {
      run.status = 'abandoned'
      return ok(run)
    }
    if (u.startsWith('/api/import/runs/run-1/files/') && method === 'POST') {
      const key = decodeURIComponent(u.split('/files/')[1])
      run.decisions.files[key] = { document_id: 'doc-1', skipped: false, equipment_key: run.decisions.files[key]?.equipment_key ?? '' }
      return ok({ document_id: 'doc-1', duplicate: false, run }, 201)
    }
    if (u === '/api/import/runs/run-1/commit' && method === 'POST') return ok(commitResponse.body, commitResponse.status)
    if (u.startsWith('/api/inventory/zones')) {
      return ok({ zones: [{ id: 'zone-existing', name: 'Flybridge (old)', sort_index: 0, bins: [] }] })
    }
    if (u.startsWith('/api/inventory/equipment')) return ok({ items: [] })
    return ok({})
  })
}

beforeEach(() => {
  run = freshRun()
  patchBodies = []
  patchFailure = null
  commitResponse = { status: 200, body: {} }
  fetchMock.mockReset()
  stubFetch()
  vi.stubGlobal('fetch', fetchMock)
})

function renderWizard() {
  const onExit = vi.fn()
  const onRunChange = vi.fn()
  render(<ImportWizard runId="run-1" onExit={onExit} onRunChange={onRunChange} />)
  return { onExit, onRunChange }
}

const entryLabel = (entry: { title: string; date: string }, kind: 'Include' | 'Equipment for') => {
  const index = run.staged.log_entries.findIndex((e) => e === entry || (e as unknown) === (entry as unknown))
  return `${kind} entry ${index + 1}: ${entry.title} ${entry.date}`
}

const next = () => fireEvent.click(screen.getByRole('button', { name: 'Next' }))

async function goToLogStep() {
  await screen.findByText('What is in the export')
  next()
  await screen.findByRole('heading', { name: 'Vessel' })
  next()
  await screen.findByRole('heading', { name: 'Locations' })
  next()
  await screen.findByRole('heading', { name: 'Equipment and spares' })
  next()
  await screen.findByRole('heading', { name: 'Maintenance log' })
}

/** Leaves every ambiguous service entry unticked so the log page lets go. */
function untickAmbiguousLogEntries() {
  for (const entry of run.staged.log_entries.filter((e) => e.candidates.length > 1)) {
    run.decisions.records[entry.key] = { action: 'skip', target_id: '' }
  }
}

describe('ImportWizard steps', () => {
  it('opens on the review page with counts, what is left behind and what to look at', async () => {
    renderWizard()

    await screen.findByText('What is in the export')
    const sections = screen.getByRole('region', { name: 'Records per section' })
    expect(within(sections).getByText('Vessel Particulars')).toBeInTheDocument()
    expect(within(sections).getByText('11 of 23')).toBeInTheDocument()

    const left = screen.getByRole('region', { name: 'Left behind' })
    expect(within(left).getByText('Handover Tips')).toBeInTheDocument()
    expect(within(left).getAllByText('Note not imported')).toHaveLength(2)

    const look = screen.getByRole('region', { name: 'Worth a look' })
    expect(within(look).getAllByText(/32 kg|displacement|implausib/i).length).toBeGreaterThan(0)
  })

  it('marks records an earlier import already wrote', async () => {
    const equipment = run.staged.equipment[3]
    run.already_imported = [equipment.key]
    renderWizard()

    await screen.findByText('What is in the export')
    expect(screen.getByText(/already in Helmcentral from an earlier import/)).toBeInTheDocument()
    next(); await screen.findByRole('heading', { name: 'Vessel' })
    next(); await screen.findByRole('heading', { name: 'Locations' })
    next(); await screen.findByRole('heading', { name: 'Equipment and spares' })
    expect(screen.getAllByText('Already imported')).toHaveLength(1)
  })

  it('saves nothing when nothing changed, and saves only the changed entries when a page is left', async () => {
    renderWizard()
    await screen.findByText('What is in the export')
    next()
    await screen.findByRole('heading', { name: 'Vessel' })
    next()
    await screen.findByRole('heading', { name: 'Locations' })
    expect(patchBodies).toHaveLength(0)

    const location = run.staged.locations[0]
    fireEvent.change(screen.getByLabelText(`What to do with ${location.name}`), { target: { value: 'skip' } })
    next()
    await screen.findByRole('heading', { name: 'Equipment and spares' })

    expect(patchBodies).toEqual([{ decisions: { zones: { [location.key]: { action: 'skip', zone_id: '' } } } }])
  })

  it('matches a location to an existing zone and sends the zone id', async () => {
    renderWizard()
    await screen.findByText('What is in the export')
    next(); await screen.findByRole('heading', { name: 'Vessel' })
    next(); await screen.findByRole('heading', { name: 'Locations' })

    const location = run.staged.locations[0]
    await screen.findAllByRole('option', { name: 'Use existing: Flybridge (old)' })
    fireEvent.change(screen.getByLabelText(`What to do with ${location.name}`), { target: { value: 'match:zone-existing' } })
    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    await screen.findByRole('heading', { name: 'Vessel' })

    expect(patchBodies[0].decisions.zones?.[location.key]).toEqual({ action: 'match', zone_id: 'zone-existing' })
  })

  it('stays on the page and shows the server reason when a save is refused', async () => {
    patchFailure = 'invalid import decision: no location "x" in this export'
    renderWizard()
    await screen.findByText('What is in the export')
    next(); await screen.findByRole('heading', { name: 'Vessel' })
    next(); await screen.findByRole('heading', { name: 'Locations' })

    fireEvent.change(screen.getByLabelText(`What to do with ${run.staged.locations[0].name}`), { target: { value: 'skip' } })
    next()

    expect(await screen.findByRole('alert')).toHaveTextContent('invalid import decision: no location "x" in this export')
    expect(screen.getByRole('heading', { name: 'Locations' })).toBeInTheDocument()
  })

  it('stores the particulars the operator ticks and never offers the live-data ones', async () => {
    renderWizard()
    await screen.findByText('What is in the export')
    next()
    await screen.findByRole('heading', { name: 'Vessel' })

    const builder = run.staged.particulars.find((p) => p.field === 'builder')!
    const tick = screen.getByRole('checkbox', { name: 'Store Brand' })
    expect(tick).toBeChecked()
    fireEvent.click(tick)
    next()
    await screen.findByRole('heading', { name: 'Locations' })
    expect(patchBodies[0].decisions.particulars?.[builder.key]).toBe('skip')

    expect(screen.queryByRole('checkbox', { name: /Store Length overall/ })).not.toBeInTheDocument()
  })

  it('shows a live-data value against the export and flags a difference', async () => {
    renderWizard()
    await screen.findByText('What is in the export')
    next()
    await screen.findByRole('heading', { name: 'Vessel' })

    const live = screen.getByRole('region', { name: 'Kept with live instrument data' })
    // Export says 17.9 m length (live 17.9 m) and 1.2 m draft (live 1.5 m).
    expect(within(live).getAllByText('Matches').length).toBeGreaterThan(0)
    expect(within(live).getAllByText('Differs').length).toBeGreaterThan(0)
    expect(within(live).getAllByText(/live instrument data stays the source/)).not.toHaveLength(0)
  })

  it('lists a locker with one line per item', async () => {
    renderWizard()
    await screen.findByText('What is in the export')
    next(); await screen.findByRole('heading', { name: 'Vessel' })
    next(); await screen.findByRole('heading', { name: 'Locations' })
    next(); await screen.findByRole('heading', { name: 'Equipment and spares' })

    const bin = run.staged.spares.find((s) => s.kind === 'bin')!
    const lines = screen.getByRole('list', { name: `Items in ${bin.name}` })
    expect(within(lines).getAllByRole('listitem')).toHaveLength(bin.items.length)
  })

  it('shows part number and on hand against required, with the shortfall', async () => {
    renderWizard()
    await screen.findByText('What is in the export')
    next(); await screen.findByRole('heading', { name: 'Vessel' })
    next(); await screen.findByRole('heading', { name: 'Locations' })
    next(); await screen.findByRole('heading', { name: 'Equipment and spares' })

    expect(screen.getByText('Part number 21669')).toBeInTheDocument()
    expect(screen.getAllByText('On hand 0 / required 1')).not.toHaveLength(0)
    expect(screen.getByText('Out of stock')).toBeInTheDocument()
  })
})

describe('ImportWizard maintenance log', () => {
  it('blocks Next until an ambiguous entry has its equipment chosen', async () => {
    renderWizard()
    await goToLogStep()

    const ambiguous = run.staged.log_entries.find((e) => e.candidates.length > 1)!
    expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled()

    const pick = screen.getByLabelText(entryLabel(ambiguous, 'Equipment for'))
    fireEvent.change(pick, { target: { value: `key:${ambiguous.candidates[0]}` } })

    // Every ambiguous entry in the fixture needs its own choice.
    for (const entry of run.staged.log_entries.filter((e) => e.candidates.length > 1)) {
      fireEvent.change(screen.getByLabelText(entryLabel(entry, 'Equipment for')), { target: { value: 'none' } })
    }
    expect(screen.getByRole('button', { name: 'Next' })).toBeEnabled()
  })

  it('offers the two engines and No equipment, and records the choice as staged keys', async () => {
    renderWizard()
    await goToLogStep()

    const ambiguous = run.staged.log_entries.find((e) => e.candidates.length > 1)!
    const pick = screen.getByLabelText(entryLabel(ambiguous, 'Equipment for'))
    const options = within(pick).getAllByRole('option').map((o) => o.textContent)
    expect(options).toEqual(['Choose the equipment', 'Cummins QSB 6.7, Port · 550 hp · Diesel', 'Cummins QSB 6.7, STBD · 550 hp · Diesel', 'No equipment'])

    fireEvent.change(pick, { target: { value: `key:${ambiguous.candidates[1]}` } })
    for (const entry of run.staged.log_entries.filter((e) => e.candidates.length > 1 && e.key !== ambiguous.key)) {
      fireEvent.change(screen.getByLabelText(entryLabel(entry, 'Equipment for')), { target: { value: 'none' } })
    }
    next()
    await screen.findByRole('heading', { name: 'Notes' })

    expect(patchBodies[0].decisions.log_equipment?.[ambiguous.key]).toEqual({ equipment_key: ambiguous.candidates[1], equipment_id: '' })
  })

  it('lets go once the ambiguous entries are unticked', async () => {
    renderWizard()
    await goToLogStep()
    // The repeated entry starts unticked already; only the others need it.
    const ambiguous = run.staged.log_entries.filter((e) => e.candidates.length > 1 && run.decisions.records[e.key]?.action === 'create')
    expect(ambiguous.length).toBeGreaterThan(0)

    for (const entry of ambiguous) fireEvent.click(screen.getByRole('checkbox', { name: entryLabel(entry, 'Include') }))

    expect(screen.getByRole('button', { name: 'Next' })).toBeEnabled()
  })
})

describe('ImportWizard notes', () => {
  it('lists a password note as not imported, with no way to tick it', async () => {
    untickAmbiguousLogEntries()
    renderWizard()
    await goToLogStep()
    next()
    await screen.findByRole('heading', { name: 'Notes' })

    const skipped = run.staged.notes.filter((n) => n.skip)
    expect(skipped.length).toBeGreaterThan(0)
    for (const note of skipped) {
      expect(screen.getByText(note.title)).toBeInTheDocument()
      expect(screen.getByText(note.skip_reason)).toBeInTheDocument()
      expect(screen.queryByRole('checkbox', { name: `Include ${note.title}` })).not.toBeInTheDocument()
    }
    expect(screen.getAllByText('Not imported')).toHaveLength(skipped.length)
  })

  it('ticks and unticks an ordinary note and a task', async () => {
    untickAmbiguousLogEntries()
    renderWizard()
    await goToLogStep()
    next()
    await screen.findByRole('heading', { name: 'Notes' })

    const task = run.staged.notes.find((n) => n.kind === 'task')!
    const box = screen.getByRole('checkbox', { name: `Include ${task.title}` })
    expect(box).toBeChecked()
    fireEvent.click(box)
    expect(box).not.toBeChecked()
    expect(screen.getByText('Task')).toBeInTheDocument()
  })
})

describe('ImportWizard photos and documents', () => {
  async function goToFirstFile() {
    untickAmbiguousLogEntries()
    renderWizard()
    await goToLogStep()
    next()
    await screen.findByRole('heading', { name: 'Notes' })
    next()
    await screen.findByRole('heading', { name: /^(Document|Photo) 1 of 3$/ })
  }

  it('shows one page per file with a safe link out', async () => {
    await goToFirstFile()

    const file = run.staged.files[0]
    const link = screen.getByRole('link', { name: /Open in YachtWave/ })
    expect(link).toHaveAttribute('href', file.url)
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(screen.getByText(file.label)).toBeInTheDocument()
    expect(screen.getByText(file.attached_to)).toBeInTheDocument()
  })

  it('blocks Next until the file is skipped', async () => {
    await goToFirstFile()
    expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: 'Skip this file' }))

    expect(screen.getByRole('button', { name: 'Next' })).toBeEnabled()
    next()
    await screen.findByRole('heading', { name: /2 of 3$/ })
    expect(patchBodies.at(-1)!.decisions.files?.[run.staged.files[0].key]).toMatchObject({ skipped: true })
  })

  it('uploads a pasted file to the slot and lets Next through', async () => {
    await goToFirstFile()
    const key = run.staged.files[0].key

    const pasted = new File(['%PDF'], 'receipt.pdf', { type: 'application/pdf' })
    fireEvent.paste(screen.getByRole('group', { name: 'Paste or drop the file here' }), { clipboardData: { files: [pasted] } })

    await screen.findByText(/File received/)
    const [, init] = fetchMock.mock.calls.find(([url]) => String(url) === `/api/import/runs/run-1/files/${key}`)!
    expect((init as RequestInit).method).toBe('POST')
    expect(((init as RequestInit).body as FormData).get('file')).toBeInstanceOf(File)
    expect(screen.getByRole('button', { name: 'Next' })).toBeEnabled()
  })

  it('uploads a dropped file', async () => {
    await goToFirstFile()
    const key = run.staged.files[0].key

    const dropped = new File(['x'], 'scan.pdf', { type: 'application/pdf' })
    fireEvent.drop(screen.getByRole('group', { name: 'Paste or drop the file here' }), { dataTransfer: { files: [dropped] } })

    await screen.findByText(/File received/)
    expect(fetchMock.mock.calls.some(([url]) => String(url) === `/api/import/runs/run-1/files/${key}`)).toBe(true)
  })

  it('shows the server message when the file is refused and keeps Next blocked', async () => {
    await goToFirstFile()
    fetchMock.mockImplementationOnce(() => Promise.resolve({ ok: false, status: 400, json: async () => ({ error: 'that photo is a HEIC file; save it as JPEG or PNG first' }) }))

    fireEvent.paste(screen.getByRole('group', { name: 'Paste or drop the file here' }), {
      clipboardData: { files: [new File(['x'], 'IMG.heic', { type: 'image/heic' })] },
    })

    expect(await screen.findByRole('alert')).toHaveTextContent('that photo is a HEIC file; save it as JPEG or PNG first')
    expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled()
  })
})

describe('ImportWizard confirm and commit', () => {
  async function goToConfirm() {
    untickAmbiguousLogEntries()
    for (const file of run.staged.files) run.decisions.files[file.key] = { document_id: '', skipped: true, equipment_key: '' }
    renderWizard()
    await goToLogStep()
    next(); await screen.findByRole('heading', { name: 'Notes' })
    next(); await screen.findByRole('heading', { name: /1 of 3$/ })
    next(); await screen.findByRole('heading', { name: /2 of 3$/ })
    next(); await screen.findByRole('heading', { name: /3 of 3$/ })
    next(); await screen.findByRole('heading', { name: 'Confirm' })
  }

  it('summarises what will be created and commits', async () => {
    commitResponse = {
      status: 200,
      body: {
        run: { ...run, status: 'committed', committed_at: '2026-10-01T06:00:00Z' },
        summary: {
          counts: { equipment: { created: 2, matched: 0, skipped: 1 } },
          records: [
            { kind: 'equipment', key: 'k1', id: 'eq-9', label: 'ZEN 100', action: 'created' },
            { kind: 'note', key: 'k2', id: 'doc-9', label: 'Alternators', action: 'created' },
          ],
        },
      },
    }
    await goToConfirm()

    expect(screen.getByRole('table')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Import' }))

    await screen.findByText('Import finished')
    expect(screen.getByRole('link', { name: 'ZEN 100' })).toHaveAttribute('href', '/inventory/equipment/eq-9')
    expect(screen.getByRole('link', { name: 'Alternators' })).toHaveAttribute('href', '/documents?document=doc-9')
  })

  it('shows a 409 refusal as the server worded it and stays on the page', async () => {
    commitResponse = { status: 409, body: { error: '1 file(s) have no decision yet (hand over the file or skip it): Mackay Marina' } }
    await goToConfirm()

    fireEvent.click(screen.getByRole('button', { name: 'Import' }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('1 file(s) have no decision yet (hand over the file or skip it): Mackay Marina'))
    expect(screen.getByRole('heading', { name: 'Confirm' })).toBeInTheDocument()
  })

})

describe('ImportConfirmStep outstanding items', () => {
  it('names an unsettled file and an unchosen log entry, each with a way back to its step', () => {
        const onGoToStep = vi.fn()
    render(<ImportConfirmStep run={run} decisions={run.decisions} setDecision={vi.fn()} onGoToStep={onGoToStep} commitError={null} />)

    const alert = screen.getByRole('alert')
    expect(within(alert).getByText(/need its equipment chosen/)).toBeInTheDocument()
    expect(within(alert).getByText(`${run.staged.files[0].label} has not been uploaded or skipped.`)).toBeInTheDocument()

    fireEvent.click(within(alert).getAllByRole('button', { name: 'Go there' })[0])
    expect(onGoToStep).toHaveBeenCalledWith('log')
    fireEvent.click(within(alert).getAllByRole('button', { name: 'Go there' })[1])
    expect(onGoToStep).toHaveBeenCalledWith(`file:${run.staged.files[0].key}`)
  })
})

describe('ImportWizard leaving', () => {
  it('asks before abandoning a draft, then deletes it and leaves', async () => {
    const { onExit } = renderWizard()
    await screen.findByText('What is in the export')

    fireEvent.click(screen.getByRole('button', { name: 'Abandon import' }))
    expect(await screen.findByText('Abandon this import?')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([, init]) => (init as RequestInit | undefined)?.method === 'DELETE')).toBe(false)

    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Abandon import' }))

    await waitFor(() => expect(onExit).toHaveBeenCalled())
    expect(fetchMock.mock.calls.some(([url, init]) => String(url) === '/api/import/runs/run-1' && (init as RequestInit | undefined)?.method === 'DELETE')).toBe(true)
  })

  it('goes back to Import from the first page without asking', async () => {
    const { onExit } = renderWizard()
    await screen.findByText('What is in the export')

    fireEvent.click(screen.getByRole('button', { name: 'Back' }))

    await waitFor(() => expect(onExit).toHaveBeenCalled())
  })
})
