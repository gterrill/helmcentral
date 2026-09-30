import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { FormSection } from '@/components/patterns/form-section'

describe('FormSection', () => {
  it('renders the heading, description and field children', () => {
    render(
      <FormSection title="Specifications & IDs" description="What you need standing in front of the thing.">
        <p>Name field goes here</p>
      </FormSection>,
    )

    expect(screen.getByText('Specifications & IDs')).toBeInTheDocument()
    expect(screen.getByText('What you need standing in front of the thing.')).toBeInTheDocument()
    expect(screen.getByText('Name field goes here')).toBeInTheDocument()
  })

  it('renders an optional header action', () => {
    render(
      <FormSection title="Documents" action={<button type="button">Add document</button>}>
        <p>content</p>
      </FormSection>,
    )
    expect(screen.getByRole('button', { name: 'Add document' })).toBeInTheDocument()
  })
})
