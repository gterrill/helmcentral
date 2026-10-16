import { useSetupStatus } from '@/hooks/use-setup-status'
import { doneSetupCount, nextSetupStep, type SetupStep } from '@/lib/setup-status'

interface SetupLineProps {
  /** False on a wall display or for a read-only session: nothing is fetched or drawn. */
  enabled: boolean
  onOpen: (step: SetupStep) => void
}

/**
 * One quiet line while first-run setup is unfinished: "Setup: 2 of 4 done ·
 * Next: House bank". It opens Settings -> SignalK at that step's control. There is
 * nothing to dismiss: it is drawn from the server's live setup status, so it
 * goes when the last step is done and returns if a step is undone later.
 */
export function SetupLine({ enabled, onOpen }: SetupLineProps) {
  const { status, loaded } = useSetupStatus(enabled)
  if (!enabled || !loaded || status.complete) return null
  const next = nextSetupStep(status) ?? status.steps.find((s) => s.state !== 'done')
  if (!next) return null

  return (
    <div className="flex min-w-0 shrink-0 items-center border-b px-2 py-1 sm:px-4">
      <button
        type="button"
        className="min-w-0 truncate text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
        onClick={() => onOpen(next)}
      >
        {`Setup: ${doneSetupCount(status)} of ${status.steps.length} done · Next: ${next.title}`}
      </button>
    </div>
  )
}
