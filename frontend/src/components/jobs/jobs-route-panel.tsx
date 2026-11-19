import { memo, useCallback } from 'react'

import { JobsPage } from '@/components/jobs/jobs-page'
import { SERVICE_SCHEDULE_LOCATION } from '@/components/jobs/service-schedule-location'
import { useNavigate } from '@/hooks/use-app-location'

interface JobsRoutePanelProps {
  canWrite: boolean
}

// Jobs (ADR 0293) wired to the app location: one page at /jobs, nothing to
// remember. It sends the operator on to an item or to the Service schedule
// through the router, so an unsaved editor can still veto the move.
// AppShell re-renders on every live delta; memo keeps this page out of it.
export const JobsRoutePanel = memo(function JobsRoutePanel({ canWrite }: JobsRoutePanelProps) {
  const navigate = useNavigate()
  const openEquipment = useCallback(
    (id: string) => { navigate({ panel: 'inventory', route: { view: 'equipment', id } }) },
    [navigate],
  )
  const openServiceSchedule = useCallback(
    () => { navigate(SERVICE_SCHEDULE_LOCATION) },
    [navigate],
  )
  return <JobsPage canWrite={canWrite} onOpenEquipment={openEquipment} onOpenServiceSchedule={openServiceSchedule} />
})
