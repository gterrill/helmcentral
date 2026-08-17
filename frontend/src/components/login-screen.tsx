import { Anchor } from 'lucide-react'
import { useCallback, useId, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

interface LoginScreenProps {
  /** Forwards to useAuth().login — resolves to whether the attempt succeeded. */
  onLogin: (username: string, password: string) => Promise<boolean>
  /**
   * The backend's own message from the most recent failed attempt, verbatim
   * (ADR 0040): it is the only clue distinguishing bad credentials from an
   * unreachable SignalK server from an unrecognised userLevel, so this
   * component renders it as-is rather than replacing it with "Login failed".
   */
  error: string | null
}

/**
 * Delegated-login form (ADR 0040 §1): credentials entered here are forwarded
 * to SignalK's own login, not checked against anything Helmcentral stores.
 * Rendered by App.tsx in place of the dashboard only while
 * `mode === 'signalk' && !authenticated` — this component itself has no
 * opinion about auth mode.
 */
export function LoginScreen({ onLogin, error }: LoginScreenProps) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const usernameId = useId()
  const passwordId = useId()

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault()
      const trimmedUsername = username.trim()
      if (trimmedUsername === '' || password === '') return

      setSubmitting(true)
      try {
        await onLogin(trimmedUsername, password)
      } finally {
        setSubmitting(false)
      }
    },
    [username, password, onLogin],
  )

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4">
      <div className="w-full max-w-sm rounded-xl border bg-card p-6 shadow-sm">
        <div className="mb-6 flex flex-col items-center gap-2 text-center">
          <Anchor className="h-8 w-8 text-primary" aria-hidden="true" />
          <h1 className="font-display text-lg font-semibold uppercase tracking-[0.1em] text-foreground">
            Helmcentral
          </h1>
          <p className="text-xs text-muted-foreground">
            Sign in with your SignalK account
          </p>
        </div>

        <form className="flex flex-col gap-4" onSubmit={(e) => { void handleSubmit(e) }}>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={usernameId}>Username</Label>
            <Input
              id={usernameId}
              name="username"
              type="text"
              autoComplete="username"
              autoFocus
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              disabled={submitting}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor={passwordId}>Password</Label>
            <Input
              id={passwordId}
              name="password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              disabled={submitting}
            />
          </div>

          {error && (
            <div
              role="alert"
              className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive"
            >
              {error}
            </div>
          )}

          <Button type="submit" aria-label="Sign in" className="mt-1" disabled={submitting}>
            {submitting ? 'Signing in…' : 'Sign in'}
          </Button>
        </form>
      </div>
    </div>
  )
}
