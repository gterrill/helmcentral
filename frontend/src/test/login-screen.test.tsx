import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'

import { LoginScreen } from '@/components/login-screen'

// LoginScreen is deliberately dumb: all it owns is the form and the pending
// state for its own submit button. Auth state (mode/user/error) lives in
// use-auth.ts and is handed down as props, per ADR 0040's frontend plan.

describe('LoginScreen', () => {
  it('renders username and password fields with a submit control', () => {
    render(<LoginScreen onLogin={vi.fn()} error={null} />)

    expect(screen.getByLabelText(/username/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /sign in|log in/i })).toBeInTheDocument()
  })

  it('submits the entered credentials to onLogin', async () => {
    const onLogin = vi.fn().mockResolvedValue(true)
    render(<LoginScreen onLogin={onLogin} error={null} />)

    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: 'skipper' } })
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: /sign in|log in/i }))

    await waitFor(() => expect(onLogin).toHaveBeenCalledWith('skipper', 'hunter2'))
  })

  it('does not call onLogin with an empty username or password', () => {
    const onLogin = vi.fn().mockResolvedValue(true)
    render(<LoginScreen onLogin={onLogin} error={null} />)

    fireEvent.click(screen.getByRole('button', { name: /sign in|log in/i }))

    expect(onLogin).not.toHaveBeenCalled()
  })

  it('disables the submit button while a login attempt is in flight', async () => {
    let resolveLogin: (value: boolean) => void = () => {}
    const onLogin = vi.fn(() => new Promise<boolean>((resolve) => { resolveLogin = resolve }))
    render(<LoginScreen onLogin={onLogin} error={null} />)

    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: 'skipper' } })
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: /sign in|log in/i }))

    await waitFor(() => expect(screen.getByRole('button', { name: /sign in|log in/i })).toBeDisabled())

    resolveLogin(true)
    await waitFor(() => expect(screen.getByRole('button', { name: /sign in|log in/i })).not.toBeDisabled())
  })

  it('surfaces the server error message verbatim — the operator needs the exact reason, not a flattened one', () => {
    render(
      <LoginScreen
        onLogin={vi.fn()}
        error='signalk reported unrecognised userLevel "superuser" — refusing to log in rather than guessing a permission tier'
      />,
    )

    expect(
      screen.getByText('signalk reported unrecognised userLevel "superuser" — refusing to log in rather than guessing a permission tier'),
    ).toBeInTheDocument()
  })

  it('shows no error banner when there is nothing to report', () => {
    render(<LoginScreen onLogin={vi.fn()} error={null} />)

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
