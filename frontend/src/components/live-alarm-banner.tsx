import { memo, type ComponentProps } from 'react'

import { AlarmBanner } from '@/components/alarm-banner'
import { FORECAST_REFRESH_SECONDS } from '@/config/app-config'
import { acknowledgeAlarm, sameShownAlarm, useAlarmListComparedOn } from '@/hooks/use-alarms'
import { useForecastWarnings } from '@/hooks/use-forecast-warnings'

/** The banner over every page: the whole active list, compared on what the banner says of each alarm. */
export const LiveAlarmBanner = memo(function LiveAlarmBanner(props: Omit<ComponentProps<typeof AlarmBanner>, 'alarms' | 'onAcknowledge' | 'forecastWarnings'>) {
  const alarms = useAlarmListComparedOn(sameShownAlarm)
  const { activeWarning } = useForecastWarnings(FORECAST_REFRESH_SECONDS)
  return <AlarmBanner {...props} alarms={alarms} onAcknowledge={acknowledgeAlarm} forecastWarnings={activeWarning} />
})
