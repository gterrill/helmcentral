import { TriangleAlert } from 'lucide-react'
import { useId, type MouseEvent, type ReactNode } from 'react'

import { Checkbox } from '@/components/ui/checkbox'
import { buttonVariants } from '@/components/ui/button'
import { FieldLegend, FieldSet } from '@/components/ui/field'
import { FormSection } from '@/components/patterns'
import { useOptionalAppRouter } from '@/hooks/use-app-location'
import { clearPendingSetupFocus, requestSetupFocus } from '@/lib/setup-status'
import { cn } from '@/lib/utils'
import { WATCH_CHANNELS, type WatchSend } from '@/lib/watches'

// ADR 0227: where a watch's report goes and when. The channels are the ones
// alarms use, switched on in Settings. SignalK is not offered: a report on the
// bus would look like an alarm.

const SETTINGS_HREF = '/settings/alarms'
const NOTIFICATIONS_ANCHOR = 'notifications'
const ALARM_SETTINGS = { panel: 'settings', route: { view: 'section', section: 'alarms' } } as const

const SEND_OPTIONS: { id: WatchSend; title: string; detail: string }[] = [
  {
    id: 'findings',
    title: 'When it finds something',
    detail: 'Only a finding the last report did not mention. A problem that stays is not sent again every hour.',
  },
  { id: 'every', title: 'Every report', detail: 'Including the ones that find nothing.' },
]

/** "A", "A and B", "A, B and C". */
function joinNames(names: string[], conjunction = 'and'): string {
  return names.length < 2 ? names.join('') : `${names.slice(0, -1).join(', ')} ${conjunction} ${names[names.length - 1]}`
}

const ASK_ADMIN = "Ask whoever runs this boat's Helmcentral to"

/**
 * The channel setup in Settings. In-app, so the move goes through the
 * unsaved-changes guard, and Settings scrolls to the section once it has drawn.
 * A move a guard holds is asked for again if the operator commits it.
 */
function SettingsLink({ children, className }: { children: ReactNode; className?: string }) {
  const router = useOptionalAppRouter()
  const go = (e: MouseEvent<HTMLAnchorElement>) => {
    if (router === null || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return
    e.preventDefault()
    requestSetupFocus(NOTIFICATIONS_ANCHOR)
    if (router.navigate(ALARM_SETTINGS)) return
    clearPendingSetupFocus()
    const stop = router.subscribe(() => {
      const { pending, location } = router.getSnapshot()
      if (pending !== null) return
      stop()
      if (location.panel === 'settings' && location.route.view === 'section' && location.route.section === 'alarms') requestSetupFocus(NOTIFICATIONS_ANCHOR)
    })
  }
  return <a href={SETTINGS_HREF} onClick={go} className={className}>{children}</a>
}

const linkClass = 'underline underline-offset-2 hover:text-foreground'

export interface WatchNotifySectionProps {
  notify: string[]
  send: WatchSend
  /** The channels switched on in Settings. */
  channelsOn: string[]
  /** False until Settings has answered; no channel is called off before then. */
  channelsLoaded: boolean
  /** Why Settings could not be read, if it could not. */
  channelsError?: string | null
  /** Settings is admin-only; without it the links to Alarm settings are plain words. */
  canAdmin: boolean
  disabled?: boolean
  onNotifyChange: (notify: string[]) => void
  onSendChange: (send: WatchSend) => void
}

export function WatchNotifySection({
  notify,
  send,
  channelsOn,
  channelsLoaded,
  channelsError,
  canAdmin,
  disabled,
  onNotifyChange,
  onSendChange,
}: WatchNotifySectionProps) {
  const toggle = (id: string, on: boolean) =>
    onNotifyChange(WATCH_CHANNELS.map((c) => c.id as string).filter((c) => (c === id ? on : notify.includes(c))))

  const sendId = useId()

  // A channel that is off in Settings keeps its row only while this watch still selects it.
  const known = channelsLoaded && !channelsError
  const isOff = (id: string) => known && !channelsOn.includes(id)
  const checking = !channelsLoaded && !channelsError
  const rows = checking ? [] : WATCH_CHANNELS.filter((c) => !isOff(c.id) || notify.includes(c.id))
  const notSetUp = WATCH_CHANNELS.filter((c) => isOff(c.id) && !notify.includes(c.id)).map((c) => c.label)
  const one = notSetUp.length === 1

  return (
    <FormSection
      title="Notifications"
      description="Where each report goes. Reports use the same channels as alarms but never show up as one."
    >
      <FieldSet>
        <FieldLegend variant="label">Send to</FieldLegend>
        {channelsError && (
          <p role="alert" className="text-xs text-destructive">
            Could not read which channels are on: {channelsError}. The choices below are saved, but may not be sent.
          </p>
        )}
        {checking && <p className="text-sm text-muted-foreground">Checking which channels are on…</p>}
        {known && rows.length === 0 ? (
          <div className="flex flex-col items-start gap-3">
            <p className="text-sm text-muted-foreground">
              Nowhere yet. Reports stay on this page until {joinNames(WATCH_CHANNELS.map((c) => c.label), 'or')} is set up for the boat.
              {!canAdmin && ` ${ASK_ADMIN} set one up in Alarm settings.`}
            </p>
            {canAdmin && <SettingsLink className={buttonVariants({ variant: 'outline', size: 'sm' })}>Set up in Alarm settings</SettingsLink>}
          </div>
        ) : (
          <div className="flex flex-col">
            {rows.map((c) => {
              const off = isOff(c.id)
              return (
                <div key={c.id} className="flex min-w-0 flex-col justify-center py-1">
                  <div className="flex min-h-11 items-center gap-3">
                    <Checkbox
                      id={`watch-notify-${c.id}`}
                      checked={notify.includes(c.id)}
                      disabled={disabled}
                      onCheckedChange={(on) => toggle(c.id, on)}
                    />
                    <label htmlFor={`watch-notify-${c.id}`} className="flex-1 text-sm font-medium text-foreground">{c.label}</label>
                  </div>
                  {off && (
                    <p className="-mt-1 flex items-start gap-1.5 pl-7 text-xs text-amber-700 dark:text-amber-400">
                      <TriangleAlert className="mt-0.5 size-3.5 shrink-0" aria-hidden />
                      <span>
                        Off for the boat, so nothing goes here.{' '}
                        {canAdmin
                          ? <SettingsLink className="font-medium underline underline-offset-2 hover:text-foreground">Turn it on</SettingsLink>
                          : `${ASK_ADMIN} turn it on in Alarm settings.`}
                      </span>
                    </p>
                  )}
                </div>
              )
            })}
          </div>
        )}
        {notSetUp.length > 0 && rows.length > 0 && (
          <p className="mt-1 border-t border-border pt-3 text-xs text-muted-foreground">
            {joinNames(notSetUp)} {one ? 'is' : 'are'} not set up for this boat.{' '}
            {canAdmin
              ? <>{one ? 'Set it up' : 'Set them up'} in <SettingsLink className={linkClass}>Alarm settings</SettingsLink>.</>
              : `${ASK_ADMIN} set ${one ? 'it' : 'them'} up in Alarm settings.`}
          </p>
        )}
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">When to send</FieldLegend>
        <div className="flex flex-col gap-2">
          {SEND_OPTIONS.map((o) => (
            <label
              key={o.id}
              className={cn(
                'flex min-h-11 cursor-pointer items-start gap-3 rounded-md border border-border p-3',
                send === o.id && 'border-primary bg-primary/5',
                disabled && 'cursor-not-allowed opacity-60',
              )}
            >
              <input
                type="radio"
                name={sendId}
                value={o.id}
                checked={send === o.id}
                disabled={disabled}
                onChange={() => onSendChange(o.id)}
                className="mt-0.5 size-4 shrink-0 accent-primary"
              />
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="text-sm font-medium text-foreground">{o.title}</span>
                <span className="text-xs text-muted-foreground">{o.detail}</span>
              </span>
            </label>
          ))}
        </div>
      </FieldSet>
    </FormSection>
  )
}
