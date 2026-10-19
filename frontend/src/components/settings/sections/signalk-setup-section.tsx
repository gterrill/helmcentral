import { Check } from 'lucide-react'

import { FormSection } from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { useOptionalAppRouter } from '@/hooks/use-app-location'
import { useSetupStatus } from '@/hooks/use-setup-status'
import type { SettingsSectionId } from '@/components/settings/settings-nav'
import { clearPendingSetupFocus, focusSetupControl, nextSetupStep, requestSetupFocus, SETUP_ACTION_LABELS, type SetupStep } from '@/lib/setup-status'

/**
 * Settings -> SignalK -> Setup: the chain from a fresh install to tiles with
 * real data. One step at a time: the steps already done and the ones waiting
 * behind the next are one muted line each, and only the next to do shows its
 * detail and a button that goes to its control. The server works the steps out
 * from live state, so nothing is dismissed here and a step that becomes undone
 * (a renumbered instance, a deleted item, a cleared role) comes back by itself.
 *
 * It reads the saved state: a choice made in Roles counts once it is saved.
 */
export function SignalKSetupSection() {
  const { status, loaded, error } = useSetupStatus()
  const next = nextSetupStep(status)
  const router = useOptionalAppRouter()

  // A step in this section focuses its control here. One that lives in another
  // section (the optional passage limits are under Mate) asks for the focus,
  // then opens that section, which acts on the request once it has drawn.
  const openStep = (step: SetupStep) => {
    if (step.target.section === 'signalk' || router === null) {
      focusSetupControl(step.target.control)
      return
    }
    const { control } = step.target
    const section = step.target.section as SettingsSectionId
    requestSetupFocus(control)
    if (router.navigate({ panel: 'settings', route: { view: 'section', section } })) return
    // A guard holds the move (unsaved edits) while the operator decides, which
    // can outlast the request. Ask again if the move is made, drop it if not.
    clearPendingSetupFocus()
    const stop = router.subscribe(() => {
      const { pending, location } = router.getSnapshot()
      if (pending !== null) return
      stop()
      if (location.panel === 'settings' && location.route.view === 'section' && location.route.section === section) requestSetupFocus(control)
    })
  }

  return (
    <FormSection title="Setup" description="What a fresh install needs before its tiles show real data. A choice counts once it is saved.">
      {error && !loaded && <p role="alert" className="text-sm text-destructive">Could not read the setup status: {error}</p>}
      {!loaded && !error && <p className="text-sm text-muted-foreground">Loading…</p>}
      {loaded && (
        <ol className="flex flex-col gap-2">
          {status.steps.map((step) => {
            if (step.state === 'done') {
              return (
                <li key={step.id} className="flex min-w-0 items-center gap-2 text-sm text-muted-foreground">
                  <Check className="h-4 w-4 shrink-0" aria-hidden />
                  <span className="min-w-0 truncate">{step.title}</span>
                  {step.optional ? (
                    <span className="shrink-0 text-[10px] font-medium uppercase tracking-[0.14em]">Optional</span>
                  ) : null}
                  <span className="sr-only">Done</span>
                </li>
              )
            }
            if (next && step.id === next.id) {
              return (
                <li key={step.id} className="flex min-w-0 flex-col gap-2 rounded-md border border-border p-3" aria-current="step">
                  <p className="text-sm font-medium text-foreground">{step.title}</p>
                  <p className="text-sm text-muted-foreground">{step.detail}</p>
                  <div>
                    <Button type="button" size="sm" variant="outline" onClick={() => openStep(step)}>
                      {SETUP_ACTION_LABELS[step.id] ?? 'Go to the control'}
                    </Button>
                  </div>
                </li>
              )
            }
            // An open optional step is offered in full, but is never the next
            // step and holds nothing up.
            if (step.optional) {
              return (
                <li key={step.id} className="flex min-w-0 flex-col gap-2 rounded-md border border-border p-3">
                  <p className="flex min-w-0 items-center gap-2 text-sm font-medium text-foreground">
                    <span className="min-w-0 truncate">{step.title}</span>
                    <span className="shrink-0 text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">Optional</span>
                  </p>
                  <p className="text-sm text-muted-foreground">{step.detail}</p>
                  <div>
                    <Button type="button" size="sm" variant="outline" onClick={() => openStep(step)}>
                      {SETUP_ACTION_LABELS[step.id] ?? 'Go to the control'}
                    </Button>
                  </div>
                </li>
              )
            }
            return (
              <li key={step.id} className="flex min-w-0 items-center gap-2 text-sm text-muted-foreground">
                <span className="h-4 w-4 shrink-0" aria-hidden />
                <span className="min-w-0 truncate">
                  {step.title}
                  {step.state === 'blocked' ? ` · ${step.detail}` : ''}
                </span>
                {step.optional ? (
                  <span className="shrink-0 text-[10px] font-medium uppercase tracking-[0.14em]">Optional</span>
                ) : null}
              </li>
            )
          })}
        </ol>
      )}
    </FormSection>
  )
}
