import { SettingsLayout } from '@/components/patterns'
import { AlarmTransportsForm } from '@/components/alarm-transports-form'
import { IgnoredAlarmsList } from '@/components/settings/sections/ignored-alarms-list'
import { IgnoredSensorsList } from '@/components/settings/sections/ignored-sensors-list'

/**
 * Notification delivery for the alarm engine, plus the two ignore
 * lists (network alarms and sensor-health checks). All own their own data (useAlarmTransports posts to
 * /api/alarm-transports, useIgnoredSensors to /api/alarms/ignored-sensors -
 * neither is the settings endpoint), so this section deliberately keeps
 * each block whole rather than folding them into the page's shared
 * RegularSettingsDraft.
 */
export function AlarmsSection() {
  return (
    <SettingsLayout title="Alarms">
      <AlarmTransportsForm />
      <IgnoredAlarmsList />
      <IgnoredSensorsList />
    </SettingsLayout>
  )
}
