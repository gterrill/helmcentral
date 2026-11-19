import { useMemo, useState, type MouseEvent } from 'react'
import { ClipboardList, Plus } from 'lucide-react'

import { EmptyState, Page, RefreshFailedNote } from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { MaintenanceRulesTable, groupRulesByStatus } from '@/components/inventory/maintenance-rules-table'
import { OpenJobsGroup } from '@/components/inventory/open-jobs-group'
import { OpenJobDialog } from '@/components/inventory/open-job-dialogs'
import { ScheduleErrorsAlert } from '@/components/inventory/schedule-errors-alert'
import { useMaintenanceRules } from '@/hooks/use-maintenance'
import { useOpenJobs } from '@/hooks/use-open-jobs'
import { SERVICE_SCHEDULE_HREF } from '@/components/jobs/service-schedule-location'
import { isPlainLeftClick } from '@/lib/plain-click'

// ADR 0293: Jobs, the "what needs doing on the boat" page. The open jobs (a
// known problem, done once) and the services that are overdue or due soon (a
// rule, done again and again) side by side, because the operator asks "what
// needs doing", not "is it a rule or a job". The full schedule, every status,
// stays under Inventory, Service schedule.

interface JobsPageProps {
  onOpenEquipment: (id: string) => void
  onOpenServiceSchedule: () => void
  canWrite?: boolean
}

const DUE_STATUSES = ['overdue', 'due_soon'] as const

// A plain click goes through the router; a click that means "new tab" is left to
// the browser, which has the address in href.
function followInApp(event: MouseEvent<HTMLAnchorElement>, go: () => void) {
  if (!isPlainLeftClick(event)) return
  event.preventDefault()
  go()
}

export function JobsPage({ onOpenEquipment, onOpenServiceSchedule, canWrite = true }: JobsPageProps) {
  const [adding, setAdding] = useState(false)
  // The page's own unfiltered reads. The sidebar badge asks the server for its
  // count instead, and the writes that change either refresh both.
  const jobs = useOpenJobs({})
  const services = useMaintenanceRules({})

  // An acknowledged service is left out, as it is from the sidebar badge: what
  // the badge counts is what this page lists. It stays under Service schedule.
  const dueGroups = useMemo(
    () => groupRulesByStatus(services.rules.filter((rule) => !rule.acknowledged), [...DUE_STATUSES]),
    [services.rules],
  )
  const openCount = useMemo(() => jobs.jobs.filter((job) => job.status === 'open').length, [jobs.jobs])

  // Empty only on a clean, finished read of both lists: an unanswered or failed
  // one says nothing about the boat, and neither does a list that could not
  // include an item whose profile is missing.
  const nothingToDo =
    !jobs.loading && !services.loading && jobs.error === null && services.error === null
    && services.scheduleErrors.length === 0 && openCount === 0 && dueGroups.length === 0

  const servicesFallback = services.error && services.rules.length === 0 ? (
    <div className="flex flex-col items-center justify-center gap-3 p-6 text-center">
      <p role="alert" className="text-sm text-destructive">{services.error}</p>
      <Button type="button" variant="outline" size="sm" onClick={() => { void services.refresh() }}>Retry</Button>
    </div>
  ) : !services.loading && dueGroups.length === 0 ? (
    // With an item's schedule unread, "none due" would contradict the alert above.
    <p className="px-4 py-3 text-sm text-muted-foreground">
      {services.scheduleErrors.length > 0 ? 'Nothing else listed is due.' : 'No services due.'}
    </p>
  ) : undefined

  // Where the open jobs would be, so the group keeps its Show closed toggle and
  // closed jobs stay reachable when there is nothing to do.
  const nothingToDoCard = (
    <EmptyState
      className="h-auto py-12"
      icon={<ClipboardList className="h-8 w-8 text-muted-foreground" aria-hidden="true" />}
      title="Nothing needs doing."
      description="Open jobs and services coming due show here."
      action={canWrite ? (
        <Button type="button" onClick={() => setAdding(true)} className="mt-2 gap-2">
          <Plus className="h-4 w-4" aria-hidden="true" />
          Add job
        </Button>
      ) : undefined}
    />
  )

  return (
    <Page
      title="Jobs"
      primaryAction={
        canWrite
          ? { label: 'Add job', icon: <Plus className="h-4 w-4" aria-hidden="true" />, onClick: () => setAdding(true) }
          : undefined
      }
    >
      <ScheduleErrorsAlert errors={services.scheduleErrors} onOpenEquipment={onOpenEquipment} />

      <OpenJobsGroup
        onOpenEquipment={onOpenEquipment}
        canWrite={canWrite}
        showAdd={false}
        empty={nothingToDo ? nothingToDoCard : undefined}
      />

      {!nothingToDo && (
        <section aria-labelledby="services-due-heading" className="flex flex-col gap-2">
          <div className="flex items-center gap-2">
            <h3 id="services-due-heading" className="text-sm font-semibold">
              Services due{dueGroups.length > 0 ? (
                <span className="ml-1.5 font-normal tabular-nums text-muted-foreground">
                  {dueGroups.reduce((sum, group) => sum + group.rules.length, 0)}
                </span>
              ) : null}
            </h3>
            <a
              href={SERVICE_SCHEDULE_HREF}
              onClick={(event) => followInApp(event, onOpenServiceSchedule)}
              className="ml-auto text-sm text-muted-foreground underline-offset-2 hover:underline"
            >
              Service schedule
            </a>
          </div>
          {services.error && services.rules.length > 0 && (
            <RefreshFailedNote reason={services.error} onRetry={() => { void services.refresh() }} />
          )}
          <MaintenanceRulesTable
            groups={dueGroups}
            labelRules={services.rules}
            loading={services.loading}
            fallback={servicesFallback}
            canWrite={canWrite}
            onOpenEquipment={onOpenEquipment}
          />
        </section>
      )}

      <OpenJobDialog open={adding} job={null} onClose={() => setAdding(false)} />
    </Page>
  )
}
