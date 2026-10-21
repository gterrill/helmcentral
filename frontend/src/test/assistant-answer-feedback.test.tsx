import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'

import { AssistantAnswerFeedback } from '@/components/assistant-answer-feedback'

function setup(rating?: { verdict: 'up' | 'down'; note: string }, onRate = vi.fn().mockResolvedValue(null)) {
  const view = render(<AssistantAnswerFeedback messageId="a1" rating={rating} onRate={onRate} />)
  return { onRate, ...view }
}

describe('AssistantAnswerFeedback', () => {
  it('offers a good and a poor answer button, neither pressed, with no note field', () => {
    setup()
    expect(screen.getByRole('button', { name: 'Good answer' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: 'Poor answer' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })

  it('saves the verdict at once when a thumb is tapped', async () => {
    const { onRate } = setup()
    fireEvent.click(screen.getByRole('button', { name: 'Poor answer' }))
    await waitFor(() => expect(onRate).toHaveBeenCalledWith('a1', 'down', ''))
  })

  it('shows the note field once a verdict is held, asking what was wrong or right', () => {
    const { rerender, onRate } = setup({ verdict: 'down', note: '' })
    expect(screen.getByRole('button', { name: 'Poor answer' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('textbox', { name: 'What was wrong' })).toBeInTheDocument()
    rerender(<AssistantAnswerFeedback messageId="a1" rating={{ verdict: 'up', note: '' }} onRate={onRate} />)
    expect(screen.getByRole('textbox', { name: 'What was good' })).toBeInTheDocument()
  })

  it('saves the note with the verdict when Save note is tapped', async () => {
    const { onRate } = setup({ verdict: 'down', note: '' })
    fireEvent.change(screen.getByRole('textbox', { name: 'What was wrong' }), { target: { value: 'wrong tank' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save note' }))
    await waitFor(() => expect(onRate).toHaveBeenCalledWith('a1', 'down', 'wrong tank'))
  })

  it('switches verdict keeping nothing of the old note, and clears on a second tap of the active thumb', async () => {
    const { onRate } = setup({ verdict: 'down', note: 'wrong tank' })
    fireEvent.click(screen.getByRole('button', { name: 'Good answer' }))
    await waitFor(() => expect(onRate).toHaveBeenCalledWith('a1', 'up', ''))
    fireEvent.click(screen.getByRole('button', { name: 'Poor answer' }))
    await waitFor(() => expect(onRate).toHaveBeenCalledWith('a1', null))
  })

  it('says so, in the server words, when saving failed', async () => {
    const onRate = vi.fn().mockResolvedValue('message not found')
    setup(undefined, onRate)
    fireEvent.click(screen.getByRole('button', { name: 'Good answer' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('message not found')
  })
})
