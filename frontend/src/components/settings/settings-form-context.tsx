import { createContext, useContext, useMemo, type ReactNode } from 'react'

import { useSettingsForm, type UseSettingsFormResult } from '@/hooks/use-settings-form'

const SettingsFormContext = createContext<UseSettingsFormResult | null>(null)

export function SettingsFormProvider({ children }: { children: ReactNode }) {
  const form = useSettingsForm()
  const value = useMemo(() => form, [form])

  return <SettingsFormContext.Provider value={value}>{children}</SettingsFormContext.Provider>
}

export function useSettingsFormContext(): UseSettingsFormResult {
  const context = useContext(SettingsFormContext)
  if (!context) {
    throw new Error('useSettingsFormContext must be used within a SettingsFormProvider')
  }
  return context
}
