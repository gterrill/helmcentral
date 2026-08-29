import { AlarmTransportsForm } from '@/components/alarm-transports-form'

/**
 * Notification delivery for the alarm engine. Unlike its sibling sections
 * this one owns its own draft and Save button (useAlarmTransports posts to
 * /api/alarm-transports, not the settings endpoint), so it deliberately
 * keeps the AlarmTransportsForm tile whole rather than being folded into
 * the page's shared RegularSettingsDraft. Only the max-width wrapper is
 * added, to line the panel up with the other sections.
 */
export function AlarmsSection() {
  return (
    <div className="mx-auto max-w-3xl">
      <AlarmTransportsForm />
    </div>
  )
}
