import { useRef, useState } from 'react'

import { SettingsPage, type SettingsPageHandle } from '@/components/settings/settings-page'
import { useNavigationGuard } from '@/hooks/use-navigation-guard'
import { useSettingsRoute } from '@/hooks/use-settings-route'
import { settingsSectionOf } from '@/lib/app-location'

interface SettingsRoutePanelProps {
  onAskMate: (question: string, options?: { newConversation?: boolean }) => void
}

// ADR 0171: Settings wired to the app location. It owns the page's navigation
// guard: leaving the Settings panel while the form holds unsaved edits asks
// first, with the page's own save behind Save and Continue. Moving between
// sections is not leaving, and is never held.
export function SettingsRoutePanel({ onAskMate }: SettingsRoutePanelProps) {
  const { route, go } = useSettingsRoute()
  const pageRef = useRef<SettingsPageHandle>(null)
  const [dirty, setDirty] = useState(false)

  useNavigationGuard(dirty
    ? {
        owner: 'settings',
        label: 'Settings',
        blocks: (_from, to) => (to.panel === 'settings' ? null : { kind: 'dirty' }),
        save: async () => { await pageRef.current?.save() },
      }
    : null)

  return (
    <SettingsPage
      ref={pageRef}
      onDirtyChange={setDirty}
      activeSectionId={settingsSectionOf(route)}
      onSectionChange={(section) => { go({ view: 'section', section }) }}
      importRunId={route.view === 'import-run' ? route.runId : null}
      onImportRunChange={(runId) => {
        go(runId ? { view: 'import-run', runId } : { view: 'section', section: 'import' })
      }}
      onAskMate={onAskMate}
    />
  )
}
