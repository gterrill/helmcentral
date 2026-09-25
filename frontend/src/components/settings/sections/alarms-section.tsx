import { AlarmTransportsForm } from '@/components/alarm-transports-form'
import { IgnoredSensorsList } from '@/components/settings/sections/ignored-sensors-list'

/**
 * Notification delivery for the alarm engine, plus the sensor-health
 * ignore list. Both own their own data (useAlarmTransports posts to
 * /api/alarm-transports, useIgnoredSensors to /api/alarms/ignored-sensors -
 * neither is the settings endpoint), so this section deliberately keeps
 * each tile whole rather than folding them into the page's shared
 * RegularSettingsDraft. Only the max-width wrapper and spacing are added,
 * to line the panels up with the other sections.
 */
export function AlarmsSection() {
  return (
    <div className="mx-auto max-w-3xl space-y-4">
      <AlarmTransportsForm />
      <div className="rounded-lg border bg-background/60 p-4">
        <IgnoredSensorsList />
      </div>
    </div>
  )
}
