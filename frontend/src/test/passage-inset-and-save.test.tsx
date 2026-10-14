import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useInsetPreference } from '@/lib/passage-inset-preference'
import { saveBlob } from '@/lib/save-blob'

function Probe({ id }: { id: string }) {
  const [on, set] = useInsetPreference()
  return <button type="button" data-testid={id} onClick={() => set(!on)}>{on ? 'on' : 'off'}</button>
}

afterEach(() => { localStorage.clear(); vi.useRealTimers() })

describe('the overview preference', () => {
  it('is one shared choice: a change in one place reaches the other at once', () => {
    render(<><Probe id="menu" /><Probe id="replay" /></>)
    expect(screen.getByTestId('replay').textContent).toBe('off')
    act(() => { screen.getByTestId('menu').click() })
    expect(screen.getByTestId('replay').textContent).toBe('on')
    act(() => { screen.getByTestId('replay').click() })
    expect(screen.getByTestId('menu').textContent).toBe('off')
    expect(localStorage.getItem('passages.insetMap')).toBe('0')
  })
})

describe('saveBlob', () => {
  it('attaches the link for the click and releases the URL only afterwards', () => {
    vi.useFakeTimers()
    const create = vi.fn().mockReturnValue('blob:x')
    const revoke = vi.fn()
    Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke })
    let attachedAtClick = false
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      attachedAtClick = document.body.contains(this)
      expect(this.download).toBe('trip.png')
    })
    saveBlob(new Blob(['x']), 'trip.png')
    expect(attachedAtClick).toBe(true)
    expect(document.querySelector('a[download]')).toBeNull()
    expect(revoke).not.toHaveBeenCalled()
    vi.advanceTimersByTime(10_000)
    expect(revoke).toHaveBeenCalledWith('blob:x')
    click.mockRestore()
  })
})
