import { SettingsLayout } from '@/components/patterns'
import { AlarmTransportsForm } from '@/components/alarm-transports-form'
import { IgnoredSensorsList } from '@/components/settings/sections/ignored-sensors-list'

/**
 * Notification delivery for the alarm engine, plus the sensor-health
 * ignore list. Both own their own data (useAlarmTransports posts to
 * /api/alarm-transports, useIgnoredSensors to /api/alarms/ignored-sensors -
 * neither is the settings endpoint), so this section deliberately keeps
 * each block whole rather than folding them into the page's shared
 * RegularSettingsDraft.
 */
export function AlarmsSection() {
  return (
    <SettingsLayout title="Alarms">
      <AlarmTransportsForm />
      <IgnoredSensorsList />
    </SettingsLayout>
  )
}
