import { useMemo } from 'react'

import type { AlarmRule } from '@/hooks/use-alarm-rules'
import { socBandsFromRules, type SocBands } from '@/lib/soc-bands'

/**
 * Thin wrapper over socBandsFromRules: App already holds the one
 * useAlarmRules() fetch (see alarms-drawer.tsx), so this hook does no
 * fetching of its own - it just re-derives the bands whenever the rule
 * list changes. The path comes from the house bank chosen in the roles.
 */
export function useSocBands(rules: readonly AlarmRule[], socPath: string | null | undefined): SocBands {
  // The path is the house bank's state of charge from the backend. With no
  // house bank chosen there is no path, and no bands.
  return useMemo(() => (socPath ? socBandsFromRules(rules, socPath) : { warnBelow: null, alarmBelow: null }), [rules, socPath])
}
