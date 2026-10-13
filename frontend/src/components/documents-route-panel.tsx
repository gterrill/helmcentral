import { useRef, useState } from 'react'

import { DocumentDetailsPage, type DocumentDetailsPageHandle } from '@/components/document-details-page'
import { DocumentsPanel } from '@/components/documents-panel'
import { useDocumentsRoute } from '@/hooks/use-documents-route'
import { useNavigationGuard } from '@/hooks/use-navigation-guard'
import type { HelpTarget } from '@/lib/help-links'

interface DocumentsRoutePanelProps {
  /** A document to open in the viewer on mount: the `?document=` deep link the
   * app was loaded with, until the operator has left Documents once. */
  initialDocumentId: string | null
  onOpenHelp: (target: HelpTarget) => void
  onOpenAssistantSettings: () => void
  onAskMate: (question: string, options?: { newConversation?: boolean }) => void
  /** The open document's name, for the Mate sheet's screen context. */
  onDetailsTitleChange: (title: string | null) => void
}

// ADR 0171: Documents wired to the app location. The listing and the Details
// page are one panel, so which renders is the route's view. The Details page
// holds an explicit Save/Discard draft and owns the navigation guard for it.
export function DocumentsRoutePanel({
  initialDocumentId,
  onOpenHelp,
  onOpenAssistantSettings,
  onAskMate,
  onDetailsTitleChange,
}: DocumentsRoutePanelProps) {
  const { route, openFolder, openSection, openDetails, close, followFolder } = useDocumentsRoute()

  if (route.view === 'details') {
    return (
      <DetailsView
        // A new document is a new page: never show the previous one's
        // fields or dirty flag while the next loads.
        key={route.id}
        documentId={route.id}
        onOpenFolder={(folderId) => { openFolder(folderId) }}
        onBack={() => { close() }}
        onFolderContextChange={followFolder}
        onTitleChange={onDetailsTitleChange}
      />
    )
  }

  return (
    <DocumentsPanel
      initialFolderId={route.folderId}
      onFolderChange={openFolder}
      initialDocumentId={initialDocumentId}
      onEditDocument={openDetails}
      initialSectionId={route.sectionId}
      onSectionChange={openSection}
      onOpenHelp={onOpenHelp}
      onOpenAssistantSettings={onOpenAssistantSettings}
      onAskMate={onAskMate}
    />
  )
}

interface DetailsViewProps {
  documentId: string
  onOpenFolder: (folderId: string | null) => void
  onBack: () => void
  onFolderContextChange: (folderId: string | null) => void
  onTitleChange: (title: string | null) => void
}

// Its own component so the draft's dirty flag and its guard live exactly as
// long as the Details page is on screen.
function DetailsView({ documentId, onOpenFolder, onBack, onFolderContextChange, onTitleChange }: DetailsViewProps) {
  const pageRef = useRef<DocumentDetailsPageHandle>(null)
  const [dirty, setDirty] = useState(false)

  // Leaving the page is any move that does not land on this same document:
  // the Documents breadcrumb, a folder crumb, Back, another panel. A Move that
  // refiles the document re-lands on it, and is not held.
  useNavigationGuard(dirty
    ? {
        owner: 'document-details',
        label: 'Details',
        blocks: (_from, to) => (
          to.panel === 'documents' && to.route.view === 'details' && to.route.id === documentId ? null : { kind: 'dirty' }
        ),
        save: async () => {
          const page = pageRef.current
          if (!page) throw new Error('The Details page is not available to save.')
          await page.save()
        },
      }
    : null)

  return (
    <DocumentDetailsPage
      ref={pageRef}
      documentId={documentId}
      onDirtyChange={setDirty}
      onOpenFolder={onOpenFolder}
      onBack={onBack}
      onFolderContextChange={onFolderContextChange}
      onTitleChange={onTitleChange}
    />
  )
}
