import { useMemo } from 'react'

import type { AlarmRule } from '@/hooks/use-alarm-rules'
import { DEFAULT_SOC_PATH, socBandsFromRules, type SocBands } from '@/lib/soc-bands'

/**
 * Thin wrapper over socBandsFromRules: App already holds the one
 * useAlarmRules() fetch (see alarms-drawer.tsx), so this hook does no
 * fetching of its own - it just re-derives the bands whenever the rule
 * list changes.
 */
export function useSocBands(rules: readonly AlarmRule[], socPath: string = DEFAULT_SOC_PATH): SocBands {
  return useMemo(() => socBandsFromRules(rules, socPath), [rules, socPath])
}
