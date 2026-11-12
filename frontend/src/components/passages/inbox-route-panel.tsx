import { memo } from 'react'

import { InboxUploadPage } from '@/components/passages/inbox-upload-page'
import { InboxView } from '@/components/passages/inbox-view'
import { useInboxRoute } from '@/hooks/use-inbox-route'
import { usePassageList } from '@/hooks/use-passages'

export interface InboxRoutePanelProps {
  canWrite: boolean
}

// The Inbox (ADR 0279) wired to the app location: the contact sheet at /inbox
// and the page that shares to it at /inbox/upload. A panel of its own because
// it files into Passages, Documents and later Equipment and Open jobs.
// AppShell re-renders on every live delta; memo keeps this page out of it.
export const InboxRoutePanel = memo(function InboxRoutePanel({ canWrite }: InboxRoutePanelProps) {
  const nav = useInboxRoute()
  // One closure for both buttons; a click event must not reach openIndex as its options.
  const openIndex = () => { nav.openIndex() }
  if (nav.route.view === 'upload') {
    return <InboxUploadPage canWrite={canWrite} onBack={openIndex} onOpenInbox={openIndex} />
  }
  return <InboxIndex canWrite={canWrite} onOpenUpload={() => { nav.openUpload() }} />
})

function InboxIndex({ canWrite, onOpenUpload }: { canWrite: boolean; onOpenUpload: () => void }) {
  const list = usePassageList()
  return <InboxView canWrite={canWrite} passages={list.passages} onOpenUpload={onOpenUpload} />
}
