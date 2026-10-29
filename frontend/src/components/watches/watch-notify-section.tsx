import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { FormSection } from '@/components/patterns'
import { WATCH_CHANNELS, WATCH_SEND_LABELS, type WatchSend } from '@/lib/watches'

// ADR 0227: where a watch's report goes and when. The channels are the ones
// alarms use, switched on in Settings. SignalK is not offered: a report on the
// bus would look like an alarm.

export interface WatchNotifySectionProps {
  notify: string[]
  send: WatchSend
  /** The channels switched on in Settings. */
  channelsOn: string[]
  /** False until Settings has answered; no channel is called off before then. */
  channelsLoaded: boolean
  /** Why Settings could not be read, if it could not. */
  channelsError?: string | null
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
  disabled,
  onNotifyChange,
  onSendChange,
}: WatchNotifySectionProps) {
  const toggle = (id: string, on: boolean) =>
    onNotifyChange(WATCH_CHANNELS.map((c) => c.id as string).filter((c) => (c === id ? on : notify.includes(c))))
  const anyOn = channelsOn.length > 0

  return (
    <FormSection
      title="Notifications"
      description="Send each report to your phone or inbox. Alarms are not affected and these never appear as alarms."
    >
      <div className="flex flex-col gap-2" role="group" aria-label="Send reports to">
        {WATCH_CHANNELS.map((c) => {
          const switchedOff = channelsLoaded && !channelsOn.includes(c.id)
          const checked = notify.includes(c.id)
          return (
            <div key={c.id} className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5">
              <Checkbox
                id={`watch-notify-${c.id}`}
                checked={checked}
                // A channel that is off in Settings can be left selected but not newly picked.
                disabled={disabled || (switchedOff && !checked)}
                onCheckedChange={(on) => toggle(c.id, on)}
              />
              <label htmlFor={`watch-notify-${c.id}`} className="text-sm font-medium text-foreground">{c.label}</label>
              {switchedOff && (
                <span className="min-w-0 text-xs text-muted-foreground">
                  {checked ? 'Off in Settings, so nothing is sent here yet.' : 'Off. Turn it on in Settings under Alarms.'}
                </span>
              )}
            </div>
          )
        })}
        {channelsError && <p role="alert" className="text-xs text-destructive">Could not read which channels are on: {channelsError}</p>}
        {channelsLoaded && !anyOn && (
          <p className="text-xs text-muted-foreground">No channel is switched on yet. Turn one on in Settings under Alarms.</p>
        )}
      </div>

      <Field>
        <FieldLabel htmlFor="watch-send">Send</FieldLabel>
        <Select value={send} disabled={disabled} onValueChange={(v) => { if (v) onSendChange(v as WatchSend) }}>
          <SelectTrigger id="watch-send" aria-label="Send" className="max-w-sm">
            <SelectValue>{(v: WatchSend) => WATCH_SEND_LABELS[v] ?? v}</SelectValue>
          </SelectTrigger>
          <SelectPopup>
            {(Object.keys(WATCH_SEND_LABELS) as WatchSend[]).map((k) => (
              <SelectItem key={k} value={k}>{WATCH_SEND_LABELS[k]}</SelectItem>
            ))}
          </SelectPopup>
        </Select>
        <FieldDescription>
          {send === 'findings'
            ? 'A report goes out when it has a finding the last one sent did not mention. A problem that stays is not sent again every hour.'
            : 'Every report goes out, even when there is nothing to report.'}
        </FieldDescription>
      </Field>
    </FormSection>
  )
}
