import { useMemo, useState } from 'react'

import type { PagerProps } from '@/components/patterns'
import { useDocument, useDocumentListing } from '@/hooks/use-documents'
import { useManualDocuments } from '@/hooks/use-manuals'
import { useNotes } from '@/hooks/use-notes'
import { documentDisplayName } from '@/lib/document-display'
import { loadDocumentsSort, orderDocuments } from '@/lib/index-order'
import { recordPager } from '@/lib/record-pager'

interface Entry {
  id: string
  label: string
}

const toEntries = (docs: readonly { id: string; title: string; filename: string }[]): Entry[] =>
  docs.map((d) => ({ id: d.id, label: documentDisplayName(d) }))

/**
 * The arrows on a document's Details page. They step through the list the
 * operator opened it from:
 * - a Manual's section, in the order the Manual reads;
 * - the Unfiled notes view, as the server lists it;
 * - otherwise the folder's own documents (those carrying the tag, when one is
 *   filtered on) in the order the listing is sorted, which is the sort this
 *   device remembers.
 * When the document isn't in that list, as with a search hit from another
 * folder or one the tag filter leaves out, they step through the document's
 * own folder in the listing's order. Search is a dialog over the listing, not
 * a list of its own.
 */
export function useDocumentPager(
  documentId: string,
  list: { folderId: string | null; tag: string | null; unfiled: boolean; sectionId: string | null },
  open: (id: string) => void,
): PagerProps {
  const { document } = useDocument(documentId)
  const inManual = list.sectionId !== null && !list.unfiled
  const manual = useManualDocuments(inManual ? list.folderId : null)
  const listing = useDocumentListing(list.folderId, list.tag, !list.unfiled && !inManual)
  const { notes, loading: notesLoading } = useNotes({ unfiledOnly: true, enabled: list.unfiled })

  // Read once: the sort is saved from the Documents list, which is not on screen
  // while this page is, and a fresh object each render would defeat the memos.
  const [sort] = useState(loadDocumentsSort)
  const primary = useMemo<{ entries: Entry[]; loaded: boolean }>(() => {
    if (list.unfiled) return { entries: toEntries(notes), loaded: !notesLoading }
    if (inManual) return { entries: (manual ?? []).map((n) => ({ id: n.id, label: n.name })), loaded: manual !== null }
    return { entries: toEntries(orderDocuments(listing.documents, sort)), loaded: listing.loaded }
  }, [list.unfiled, inManual, notes, notesLoading, manual, listing.documents, listing.loaded, sort])

  const missing = primary.loaded && document !== null && !primary.entries.some((e) => e.id === documentId)
  const own = useDocumentListing(document?.folder_id ?? null, null, missing)
  const entries = useMemo(
    () => (missing ? toEntries(orderDocuments(own.documents, sort)) : primary.entries),
    [missing, own.documents, primary.entries, sort],
  )

  return useMemo(() => recordPager(entries, documentId, (e) => e.id, (e) => e.label, open), [entries, documentId, open])
}
