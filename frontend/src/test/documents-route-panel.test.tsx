/**
 * Documents' route wiring and the Details page's navigation guard (ADR 0171),
 * with the listing and the page stubbed.
 */
import { forwardRef, useImperativeHandle } from 'react'
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { DocumentsRoutePanel } from '@/components/documents-route-panel'
import { testRouter } from './helpers/router'

const save = vi.fn().mockResolvedValue(undefined)
const detailsRef = vi.hoisted(() => ({ detach: false }))

vi.mock('@/components/documents-panel', () => ({
  DocumentsPanel: (props: {
    initialFolderId?: string | null
    onFolderChange?: (id: string | null) => void
    initialSectionId?: string | null
    onSectionChange?: (id: string | null) => void
    onEditDocument?: (id: string) => void
    initialDocumentId?: string | null
  }) => (
    <div>
      <p data-testid="listing">{`folder=${props.initialFolderId ?? '-'} section=${props.initialSectionId ?? '-'} viewer=${props.initialDocumentId ?? '-'}`}</p>
      <button type="button" onClick={() => props.onFolderChange?.('f2')}>open-f2</button>
      <button type="button" onClick={() => props.onSectionChange?.('s3')}>open-s3</button>
      <button type="button" onClick={() => props.onEditDocument?.('doc-9')}>details-doc-9</button>
    </div>
  ),
}))

vi.mock('@/components/document-details-page', () => ({
  DocumentDetailsPage: forwardRef(function DetailsStub(
    props: {
      documentId: string
      onDirtyChange?: (dirty: boolean) => void
      onBack: () => void
      onOpenFolder: (id: string | null) => void
      onFolderContextChange?: (id: string | null) => void
      onTitleChange?: (title: string | null) => void
    },
    ref,
  ) {
    useImperativeHandle(ref, () => (detailsRef.detach ? (null as never) : { save }))
    return (
      <div>
        <p data-testid="details">{props.documentId}</p>
        <button type="button" onClick={() => props.onDirtyChange?.(true)}>make-dirty</button>
        <button type="button" onClick={props.onBack}>crumb-back</button>
        <button type="button" onClick={() => props.onOpenFolder('f5')}>crumb-folder</button>
        <button type="button" onClick={() => props.onFolderContextChange?.('f7')}>moved-to-f7</button>
        <button type="button" onClick={() => props.onTitleChange?.('Tide tables')}>named</button>
      </div>
    )
  }),
}))

function mount(path: string, overrides: Partial<Parameters<typeof DocumentsRoutePanel>[0]> = {}) {
  const harness = testRouter(path)
  const onDetailsTitleChange = vi.fn()
  render(
    <DocumentsRoutePanel
      initialDocumentId={null}
      onOpenHelp={vi.fn()}
      onOpenAssistantSettings={vi.fn()}
      onAskMate={vi.fn()}
      onDetailsTitleChange={onDetailsTitleChange}
      {...overrides}
    />,
    { wrapper: harness.wrapper as never },
  )
  return { ...harness, onDetailsTitleChange }
}

describe('DocumentsRoutePanel listing', () => {
  it('seeds the listing from the location and the viewer deep link from App', () => {
    mount('/documents?folder=f1&section=s1', { initialDocumentId: 'd1' })
    expect(screen.getByTestId('listing')).toHaveTextContent('folder=f1 section=s1 viewer=d1')
  })

  it('opens a folder, section and Details page through the location', () => {
    const { history } = mount('/documents')
    fireEvent.click(screen.getByText('open-f2'))
    expect(history.path()).toBe('/documents?folder=f2')
    fireEvent.click(screen.getByText('open-s3'))
    expect(history.path()).toBe('/documents?folder=f2&section=s3')
    fireEvent.click(screen.getByText('details-doc-9'))
    expect(history.path()).toBe('/documents/doc-9?folder=f2')
    expect(screen.getByTestId('details')).toHaveTextContent('doc-9')
  })
})

describe('DocumentsRoutePanel Details page guard', () => {
  it('registers a guard only while the draft is dirty', () => {
    const { router } = mount('/documents/doc-1')
    expect(router.hasGuards()).toBe(false)
    fireEvent.click(screen.getByText('make-dirty'))
    expect(router.getSnapshot().guardOwners).toEqual(['document-details'])
  })

  it('holds a move to another panel, naming the Details page', () => {
    const { router } = mount('/documents/doc-1')
    fireEvent.click(screen.getByText('make-dirty'))
    act(() => { router.navigate({ panel: 'forecast' }) })
    expect(router.getSnapshot().pending).toMatchObject({ kind: 'dirty', label: 'Details' })
  })

  it('holds the breadcrumb Back and a folder crumb, which stay in Documents', () => {
    const { router, history } = mount('/documents/doc-1')
    fireEvent.click(screen.getByText('make-dirty'))
    fireEvent.click(screen.getByText('crumb-back'))
    expect(router.getSnapshot().pending?.target).toMatchObject({ panel: 'documents', route: { view: 'browse' } })
    expect(history.path()).toBe('/documents/doc-1')
    act(() => { router.cancelPending() })
    fireEvent.click(screen.getByText('crumb-folder'))
    expect(router.getSnapshot().pending?.target).toMatchObject({ route: { view: 'browse', folderId: 'f5' } })
  })

  it('does not hold a Move that refiles the open document, and rewrites the entry', () => {
    const { router, history } = mount('/documents/doc-1')
    fireEvent.click(screen.getByText('make-dirty'))
    fireEvent.click(screen.getByText('moved-to-f7'))
    expect(router.getSnapshot().pending).toBeNull()
    expect(history.entries).toEqual(['/documents/doc-1?folder=f7'])
  })

  it('Save and Continue saves through the page\'s own handle', async () => {
    const { router } = mount('/documents/doc-1')
    fireEvent.click(screen.getByText('make-dirty'))
    act(() => { router.navigate({ panel: 'forecast' }) })
    await act(async () => { await router.saveAndContinue() })
    expect(save).toHaveBeenCalledTimes(1)
    expect(router.getSnapshot().location.panel).toBe('forecast')
  })

  it('Save and Continue stays put and keeps the move held when the page\'s save fails', async () => {
    save.mockRejectedValueOnce(new Error('title already used'))
    const { router } = mount('/documents/doc-1')
    fireEvent.click(screen.getByText('make-dirty'))
    act(() => { router.navigate({ panel: 'forecast' }) })
    await act(async () => { await expect(router.saveAndContinue()).rejects.toThrow('title already used') })
    expect(router.getSnapshot().pending).toMatchObject({ kind: 'dirty' })
    expect(router.getSnapshot().location.panel).toBe('documents')
  })

  it('Save and Continue fails rather than moving on when the page is not mounted to save', async () => {
    detailsRef.detach = true
    try {
      const { router } = mount('/documents/doc-1')
      fireEvent.click(screen.getByText('make-dirty'))
      act(() => { router.navigate({ panel: 'forecast' }) })
      await act(async () => { await expect(router.saveAndContinue()).rejects.toThrow(/not available/i) })
      expect(router.getSnapshot().location.panel).toBe('documents')
    } finally {
      detailsRef.detach = false
    }
  })

  it('goes straight back when clean, to the listing the page was opened from', () => {
    const { router, history } = mount('/documents/doc-1?folder=f1')
    fireEvent.click(screen.getByText('crumb-back'))
    expect(router.getSnapshot().pending).toBeNull()
    expect(history.path()).toBe('/documents?folder=f1')
  })

  it('reports the document name up for the Mate sheet', () => {
    const { onDetailsTitleChange } = mount('/documents/doc-1')
    fireEvent.click(screen.getByText('named'))
    expect(onDetailsTitleChange).toHaveBeenCalledWith('Tide tables')
  })
})
