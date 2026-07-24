import { createContext, useContext, useMemo, type ReactNode } from 'react'

import { useSecretsStatus, type UseSecretsStatusResult } from '@/hooks/use-secrets-status'

const SecretsStatusContext = createContext<UseSecretsStatusResult | null>(null)

export function SecretsStatusProvider({ children }: { children: ReactNode }) {
  const secrets = useSecretsStatus()
  const value = useMemo(() => secrets, [secrets])

  return <SecretsStatusContext.Provider value={value}>{children}</SecretsStatusContext.Provider>
}

export function useSecretsStatusContext(): UseSecretsStatusResult {
  const context = useContext(SecretsStatusContext)
  if (!context) {
    throw new Error('useSecretsStatusContext must be used within a SecretsStatusProvider')
  }
  return context
}
