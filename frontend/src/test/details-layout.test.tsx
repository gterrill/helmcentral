import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { DetailsLayout } from '@/components/patterns/details-layout'

describe('DetailsLayout', () => {
  it('renders the main content', () => {
    render(
      <DetailsLayout>
        <p>Main column content</p>
      </DetailsLayout>,
    )
    expect(screen.getByText('Main column content')).toBeInTheDocument()
  })

  it('renders an aside alongside the main content when given', () => {
    render(
      <DetailsLayout aside={<p>Aside content</p>}>
        <p>Main column content</p>
      </DetailsLayout>,
    )
    expect(screen.getByText('Main column content')).toBeInTheDocument()
    expect(screen.getByText('Aside content')).toBeInTheDocument()
  })

  it('omits the aside column entirely when none is given', () => {
    const { container } = render(
      <DetailsLayout>
        <p>Main column content</p>
      </DetailsLayout>,
    )
    // Only one child column should be present - the main one.
    expect(container.querySelectorAll(':scope > div > div').length).toBe(1)
  })

  it('renders the aside as a complementary landmark', () => {
    render(
      <DetailsLayout aside={<p>Aside content</p>}>
        <p>Main column content</p>
      </DetailsLayout>,
    )
    expect(screen.getByRole('complementary')).toHaveTextContent('Aside content')
  })

  it('does not reorder the columns by default - the aside stays visually after main below lg', () => {
    render(
      <DetailsLayout aside={<p>Aside content</p>}>
        <p>Main column content</p>
      </DetailsLayout>,
    )
    expect(screen.getByRole('complementary')).not.toHaveClass('order-first')
  })

  it('puts the aside first with asidePosition="start", still only below lg', () => {
    render(
      <DetailsLayout aside={<p>Aside content</p>} asidePosition="start">
        <p>Main column content</p>
      </DetailsLayout>,
    )
    // order-first below lg; lg:order-none cancels it back to source order
    // (main, then aside) at lg and up, where the grid already gives the
    // aside its own column regardless of DOM order.
    expect(screen.getByRole('complementary')).toHaveClass('order-first', 'lg:order-none')
  })
})
