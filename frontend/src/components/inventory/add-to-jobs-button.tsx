import { useState } from 'react'
import { ListPlus } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { OpenJobDialog } from '@/components/inventory/open-job-dialogs'
import type { OpenJobDraft } from '@/hooks/use-open-jobs'

// ADR 0270: "Add to jobs" on a watch report finding, an alarm card and a
// checklist line. It opens the Add job form already filled in, with where the
// job came from and a link back; nothing is saved until the operator taps Save.

interface AddToJobsButtonProps {
  /** What is being added, for the button's accessible name: "Add <what> to jobs". */
  what: string
  draft: OpenJobDraft
  className?: string
  /** Show "Add to jobs" beside the icon from the sm width up. Left off, the button is the icon alone. */
  showLabel?: boolean
}

export function AddToJobsButton({ what, draft, className, showLabel = false }: AddToJobsButtonProps) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button type="button" variant="ghost" size="sm" className={className} aria-label={`Add ${what} to jobs`} title="Add to jobs" onClick={() => setOpen(true)}>
        <ListPlus className="h-3.5 w-3.5" data-icon="inline-start" aria-hidden="true" />
        {showLabel && <span className="hidden sm:inline">Add to jobs</span>}
      </Button>
      <OpenJobDialog open={open} job={null} draft={draft} onClose={() => setOpen(false)} />
    </>
  )
}
