import { describe, it, expect } from 'vitest'
import { screenContextFor } from '@/lib/mate-screen'
import type { AppLocation } from '@/lib/app-location'

// ADR 0093 voice phase: screenContextFor turns the shell's current
// AppLocation (plus the active dashboard page's name, which AppLocation
// doesn't carry) into the `screen` object sent with a Mate question.

const settings = (section: 'general' | 'anchor-watch'): AppLocation => ({ panel: 'settings', route: { view: 'section', section } })
const documents = (route: 'browse' | 'details'): AppLocation => ({
  panel: 'documents',
  route: route === 'details'
    ? { view: 'details', id: 'doc-1', folderId: null, sectionId: null }
    : { view: 'browse', folderId: null, sectionId: null, documentId: null },
})

describe('screenContextFor', () => {
  it('names a plain panel, such as Forecast', () => {
    expect(screenContextFor({ panel: 'forecast' }, null)).toEqual({ panel: 'forecast' })
  })

  // The Mate panel tells Mate nothing about what the operator is working
  // on, and naming it would read as though the question were about Mate.
  it('gives no screen on the Mate panel itself', () => {
    expect(screenContextFor({ panel: 'assistant', conversationId: null }, null)).toBeUndefined()
  })

  it('names the settings section', () => {
    expect(screenContextFor(settings('anchor-watch'), null)).toEqual({
      panel: 'settings',
      section: 'anchor-watch',
    })
  })

  it('names the Import section while a wizard run is open', () => {
    expect(screenContextFor({ panel: 'settings', route: { view: 'import-run', runId: 'new' } }, null)).toEqual({
      panel: 'settings',
      section: 'import',
    })
  })

  it('names the active dashboard page when on the dashboard', () => {
    expect(screenContextFor({ panel: null, pageId: 'p1' }, 'Anchored')).toEqual({ page: 'Anchored' })
  })

  it('returns an empty context on the dashboard before any page name is known', () => {
    expect(screenContextFor({ panel: null, pageId: null }, null)).toEqual({})
  })

  it('names the document open on its Details page', () => {
    expect(screenContextFor(documents('details'), null, 'Tide tables')).toEqual({
      panel: 'documents',
      document_id: 'doc-1',
      document_title: 'Tide tables',
    })
  })

  it('sends the document id alone while its title has not loaded', () => {
    expect(screenContextFor(documents('details'), null, null)).toEqual({
      panel: 'documents',
      document_id: 'doc-1',
    })
  })

  it('does not name a document on the ordinary Documents listing', () => {
    expect(screenContextFor(documents('browse'), null, 'Stale title')).toEqual({ panel: 'documents' })
  })
})
