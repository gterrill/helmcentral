import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { FormRow } from '@/components/patterns/form-row'

describe('FormRow', () => {
  it('renders its fields', () => {
    render(
      <FormRow>
        <label>Manufacturer<input /></label>
        <label>Model<input /></label>
      </FormRow>,
    )
    expect(screen.getByLabelText('Manufacturer')).toBeInTheDocument()
    expect(screen.getByLabelText('Model')).toBeInTheDocument()
  })

  // The pair must key off the width of the container it sits in (a phone,
  // or the Details aside), not the viewport - ui/field's own
  // orientation="responsive" does the latter's cousin (@md/field-group) but
  // needs a FieldGroup ancestor that carries @container/field-group, which
  // nothing in this app provides, so it never fired. FormRow brings its own
  // container: an outer @container wrapper (a container cannot be styled by
  // its own query) around the grid that queries it.
  it('is a container-query grid: two minmax(0,1fr) columns once its container is wide enough, one otherwise', () => {
    render(
      <FormRow>
        <label>A<input /></label>
        <label>B<input /></label>
      </FormRow>,
    )
    const grid = screen.getByLabelText('A').closest('label')!.parentElement!
    expect(grid).toHaveClass('grid', 'gap-4', '@md:grid-cols-2')
    expect(grid).not.toHaveClass('grid-cols-2')
    const container = grid.parentElement!
    expect(container).toHaveClass('@container')
    expect(container).toHaveAttribute('data-slot', 'form-row')
  })

  it('lets a caller add classes to the outer wrapper', () => {
    const { container } = render(
      <FormRow className="mt-2">
        <label>A<input /></label>
      </FormRow>,
    )
    expect(container.querySelector('[data-slot="form-row"]')).toHaveClass('mt-2')
  })
})
