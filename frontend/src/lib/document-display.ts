import type { DocumentRecord } from '@/hooks/use-documents'

// ADR 0106 F1 / ADR 0115: small display helpers the Documents panel and the
// Details page both need. The first three were moved out of
// documents-panel.tsx verbatim rather than duplicated - the Details page
// (document-details-page.tsx) shows the same filename/size/MIME facts the
// listing does, just laid out as a definition list instead of table cells,
// and a second copy of these would drift the moment one of them picks up a
// new MIME type or unit. formatDocumentTime below is Details-page only so
// far, but lives here rather than in the component file because it is a
// display helper of the same kind, not page logic.

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function documentDisplayName(doc: DocumentRecord): string {
  return doc.title.trim() !== '' ? doc.title : doc.filename
}

/**
 * documentFailureMessage turns a failed document's stage and mime into an
 * operator-facing sentence with a next step, instead of the raw error
 * documents.error stores (an implementation detail - a "panic", an
 * internal on-disk storage path, a library's own error text - with no
 * indication of what to do about it). Callers keep the raw stored error
 * available alongside this (documents-panel.tsx's row puts it in a title
 * attribute, document-details-page.tsx under an "Error details"
 * disclosure) - this function only ever supplies the headline sentence,
 * and never touches the stored error string itself.
 *
 * doc.stage stays whatever it was when the document failed (SetFailed,
 * backend/documents_store.go, leaves it alone) - "extract" means the local
 * text-reading step failed (a PDF's text layer, a text file over the size
 * cap, ...); anything else ("enrich") means extraction succeeded and a
 * later, Mate-assisted step (summarising, OCR) is what failed.
 */
export function documentFailureMessage(doc: { stage: string; mime: string }): string {
  if (doc.stage === 'extract') {
    if (doc.mime === 'application/pdf') {
      return "Couldn't read the text in this PDF. Try Reindex; if it fails again, open it in a PDF viewer, save a copy and upload that."
    }
    if (doc.mime.startsWith('image/')) {
      return "Couldn't read this image. Try Reindex, or upload it again."
    }
    return "Couldn't read this file. Try Reindex, or upload it again."
  }
  return "Mate couldn't finish indexing this document. Try Reindex."
}

export function mimeLabel(mime: string): string {
  if (mime === 'application/pdf') return 'PDF'
  if (mime.startsWith('image/')) return 'Image'
  if (mime === 'text/csv') return 'CSV'
  if (mime === 'application/json') return 'JSON'
  if (mime === 'text/markdown') return 'Markdown'
  if (mime.startsWith('text/')) return 'Text'
  return 'File'
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

/**
 * "D Mon YYYY HH:MM" for the Details page's Uploaded/Last indexed rows.
 * alarm-display.ts's formatAlarmTime is built for an alarm raised minutes
 * ago (bare HH:MM for today, no year at all otherwise) - a document library
 * is the opposite case, where "uploaded eighteen months back" is the normal
 * reading, so this always carries the full date. Same null-not-a-placeholder
 * contract as formatAlarmTime: a missing or unparseable value is null, and
 * the caller decides what that reads as ("--", "Not yet", ...).
 */
export function formatDocumentTime(iso: string | null | undefined): string | null {
  if (!iso) return null
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return null

  const day = date.getDate()
  const month = MONTHS[date.getMonth()]
  const year = date.getFullYear()
  const hh = String(date.getHours()).padStart(2, '0')
  const mm = String(date.getMinutes()).padStart(2, '0')
  return `${day} ${month} ${year} ${hh}:${mm}`
}
