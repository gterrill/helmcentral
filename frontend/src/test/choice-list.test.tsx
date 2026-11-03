import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { ChoiceCheckbox, ChoiceList, type ChoiceListChoice } from '@/components/patterns'

describe('ChoiceCheckbox', () => {
  it('is named by its label only and is not in a group', () => {
    render(<ChoiceCheckbox label="Alpha" details="What alpha means." checked={false} onCheckedChange={() => {}} />)
    expect(screen.getByRole('checkbox', { name: 'Alpha' })).toBeInTheDocument()
    expect(screen.queryByRole('group')).not.toBeInTheDocument()
  })

  it('is described by details, and by selected content while checked', () => {
    const props = { label: 'Alpha', details: 'Plain.', selectedContent: <p>Off for the boat.</p>, onCheckedChange: () => {} }
    const { rerender } = render(<ChoiceCheckbox {...props} checked />)
    expect(screen.getByRole('checkbox', { name: 'Alpha' })).toHaveAccessibleDescription('Plain. Off for the boat.')
    rerender(<ChoiceCheckbox {...props} checked={false} />)
    expect(screen.getByRole('checkbox', { name: 'Alpha' })).toHaveAccessibleDescription('Plain.')
    expect(screen.queryByText('Off for the boat.')).not.toBeInTheDocument()
  })

  it('calls onCheckedChange once for a click on the label', () => {
    const onCheckedChange = vi.fn()
    render(<ChoiceCheckbox label="Alpha" details="Tap me." checked={false} onCheckedChange={onCheckedChange} />)
    fireEvent.click(screen.getByText('Alpha').closest('label')!)
    expect(onCheckedChange).toHaveBeenCalledTimes(1)
    expect(onCheckedChange).toHaveBeenCalledWith(true)
  })

  it('calls onCheckedChange once with the new value for a click on the box', () => {
    const onCheckedChange = vi.fn()
    render(<ChoiceCheckbox label="Alpha" checked onCheckedChange={onCheckedChange} />)
    fireEvent.click(screen.getByRole('checkbox', { name: 'Alpha' }))
    expect(onCheckedChange).toHaveBeenCalledTimes(1)
    expect(onCheckedChange).toHaveBeenCalledWith(false)
  })

  it('does not toggle when disabled, and dims the row', () => {
    const onCheckedChange = vi.fn()
    render(<ChoiceCheckbox disabled label="Alpha" checked={false} onCheckedChange={onCheckedChange} />)
    const box = screen.getByRole('checkbox', { name: 'Alpha' })
    expect(box).toHaveAttribute('aria-disabled', 'true')
    expect(box.parentElement).toHaveClass('opacity-60')
    fireEvent.click(screen.getByText('Alpha'))
    expect(onCheckedChange).not.toHaveBeenCalled()
  })

  it('puts id on the input the label points at', () => {
    const { container } = render(<ChoiceCheckbox id="my-option" label="Alpha" checked={false} onCheckedChange={() => {}} />)
    expect(container.querySelector('input#my-option')).not.toBeNull()
    expect(screen.getByText('Alpha').closest('label')).toHaveAttribute('for', 'my-option')
  })

  it('shows secondary content always, and clicking it does not toggle', () => {
    const onCheckedChange = vi.fn()
    render(<ChoiceCheckbox label="Alpha" secondaryContent={<button type="button">Extra</button>} checked={false} onCheckedChange={onCheckedChange} />)
    fireEvent.click(screen.getByRole('button', { name: 'Extra' }))
    expect(onCheckedChange).not.toHaveBeenCalled()
  })
})

const CHOICES: ChoiceListChoice[] = [
  { value: 'a', label: 'Alpha' },
  { value: 'b', label: 'Bravo' },
  { value: 'c', label: 'Charlie' },
]

describe('ChoiceList', () => {
  it('is a group named by its label', () => {
    render(<ChoiceList label="Pick" choices={CHOICES} value="a" onChange={() => {}} />)
    expect(screen.getByRole('group', { name: 'Pick' })).toHaveAttribute('data-slot', 'choice-list')
  })

  it('single: selects by radio and reports the value', () => {
    const onChange = vi.fn()
    render(<ChoiceList label="Pick" choices={CHOICES} value="a" onChange={onChange} />)
    expect(screen.getByRole('radio', { name: 'Alpha' })).toBeChecked()
    expect(screen.getByRole('radio', { name: 'Bravo' })).not.toBeChecked()
    fireEvent.click(screen.getByRole('radio', { name: 'Bravo' }))
    expect(onChange).toHaveBeenCalledWith('b')
  })

  it('multiple: toggles on and off and returns the values in choices order', () => {
    const onChange = vi.fn()
    const { rerender } = render(<ChoiceList multiple label="Pick" choices={CHOICES} value={['c']} onChange={onChange} />)
    fireEvent.click(screen.getByRole('checkbox', { name: 'Alpha' }))
    expect(onChange).toHaveBeenLastCalledWith(['a', 'c'])
    rerender(<ChoiceList multiple label="Pick" choices={CHOICES} value={['a', 'c']} onChange={onChange} />)
    fireEvent.click(screen.getByRole('checkbox', { name: 'Charlie' }))
    expect(onChange).toHaveBeenLastCalledWith(['a'])
  })

  it('multiple: drops a stored value that has no row, since nothing on screen could untick it', () => {
    const onChange = vi.fn()
    render(<ChoiceList multiple label="Pick" choices={CHOICES} value={['gone', 'c']} onChange={onChange} />)
    fireEvent.click(screen.getByRole('checkbox', { name: 'Bravo' }))
    expect(onChange).toHaveBeenLastCalledWith(['b', 'c'])
    fireEvent.click(screen.getByRole('checkbox', { name: 'Charlie' }))
    expect(onChange).toHaveBeenLastCalledWith([])
  })

  it('always shows details, and the control is described by them but named by the title only', () => {
    render(
      <ChoiceList
        multiple
        label="Pick"
        choices={[{ value: 'a', label: 'Alpha', details: 'What alpha means.' }]}
        value={[]}
        onChange={() => {}}
      />,
    )
    const box = screen.getByRole('checkbox', { name: 'Alpha' })
    expect(screen.getByText('What alpha means.')).toBeVisible()
    expect(box).toHaveAccessibleDescription('What alpha means.')
  })

  it('single: details describe the radio too', () => {
    render(
      <ChoiceList label="Pick" choices={[{ value: 'a', label: 'Alpha', details: 'About alpha.' }]} value="a" onChange={() => {}} />,
    )
    expect(screen.getByRole('radio', { name: 'Alpha' })).toHaveAccessibleDescription('About alpha.')
  })

  it('shows selectedContent only while selected, and clicking in it does not toggle', () => {
    const onChange = vi.fn()
    const choices: ChoiceListChoice[] = [{ value: 'a', label: 'Alpha', selectedContent: <button type="button">Follow up</button> }]
    const { rerender } = render(<ChoiceList multiple label="Pick" choices={choices} value={[]} onChange={onChange} />)
    expect(screen.queryByRole('button', { name: 'Follow up' })).not.toBeInTheDocument()
    rerender(<ChoiceList multiple label="Pick" choices={choices} value={['a']} onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: 'Follow up' }))
    expect(onChange).not.toHaveBeenCalled()
  })

  it('single: selectedContent follows the selected radio', () => {
    const choices: ChoiceListChoice[] = [
      { value: 'a', label: 'Alpha', selectedContent: <p>Only for alpha</p> },
      { value: 'b', label: 'Bravo', selectedContent: <p>Only for bravo</p> },
    ]
    render(<ChoiceList label="Pick" choices={choices} value="b" onChange={() => {}} />)
    expect(screen.queryByText('Only for alpha')).not.toBeInTheDocument()
    expect(screen.getByText('Only for bravo')).toBeInTheDocument()
  })

  it('clicking the details text toggles the choice', () => {
    const onChange = vi.fn()
    render(
      <ChoiceList multiple label="Pick" choices={[{ value: 'a', label: 'Alpha', details: 'Tap me.' }]} value={[]} onChange={onChange} />,
    )
    fireEvent.click(screen.getByText('Tap me.'))
    expect(onChange).toHaveBeenCalledWith(['a'])
  })

  it('clicking the title toggles a single choice too', () => {
    const onChange = vi.fn()
    render(<ChoiceList label="Pick" choices={CHOICES} value="a" onChange={onChange} />)
    fireEvent.click(screen.getByText('Bravo'))
    expect(onChange).toHaveBeenCalledWith('b')
  })

  it('a disabled list wins over a choice that is not disabled', () => {
    const onChange = vi.fn()
    render(<ChoiceList multiple disabled label="Pick" choices={CHOICES} value={[]} onChange={onChange} />)
    for (const box of screen.getAllByRole('checkbox')) expect(box).toHaveAttribute('aria-disabled', 'true')
    fireEvent.click(screen.getByText('Alpha'))
    expect(onChange).not.toHaveBeenCalled()
  })

  it('disables a single choice and leaves the others', () => {
    const onChange = vi.fn()
    render(
      <ChoiceList
        multiple
        label="Pick"
        choices={[{ value: 'a', label: 'Alpha', disabled: true }, { value: 'b', label: 'Bravo' }]}
        value={[]}
        onChange={onChange}
      />,
    )
    expect(screen.getByRole('checkbox', { name: 'Alpha' })).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByRole('checkbox', { name: 'Bravo' })).not.toHaveAttribute('aria-disabled', 'true')
    fireEvent.click(screen.getByText('Alpha'))
    expect(onChange).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText('Bravo'))
    expect(onChange).toHaveBeenCalledWith(['b'])
  })

  it('single: a disabled radio is disabled', () => {
    render(
      <ChoiceList label="Pick" choices={[{ value: 'a', label: 'Alpha', disabled: true }]} value="" onChange={() => {}} />,
    )
    expect(screen.getByRole('radio', { name: 'Alpha' })).toBeDisabled()
  })

  it('makes the whole row the tap target, including the gap beside a radio', () => {
    const onChange = vi.fn()
    render(<ChoiceList label="Pick" choices={[{ value: 'a', label: 'Alpha' }, { value: 'b', label: 'Bravo' }]} value="a" onChange={onChange} />)
    const label = screen.getByText('Bravo').closest('label')!
    expect(label).toHaveClass('after:absolute', 'after:inset-0')
    expect(screen.getByRole('radio', { name: 'Bravo' }).parentElement).toHaveClass('relative')
    fireEvent.click(label)
    expect(onChange).toHaveBeenCalledWith('b')
  })

  it('toggles a checkbox once, whether the box or its row is clicked', () => {
    const onChange = vi.fn()
    render(<ChoiceList multiple label="Pick" choices={[{ value: 'a', label: 'Alpha' }]} value={[]} onChange={onChange} />)
    const box = screen.getByRole('checkbox', { name: 'Alpha' })
    fireEvent.click(box)
    expect(onChange).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByText('Alpha').closest('label')!)
    expect(onChange).toHaveBeenCalledTimes(2)
  })

  it('dims the control along with the text of a disabled row', () => {
    render(<ChoiceList multiple disabled label="Pick" choices={[{ value: 'a', label: 'Alpha' }]} value={['a']} onChange={() => {}} />)
    expect(screen.getByRole('checkbox', { name: 'Alpha' }).parentElement).toHaveClass('opacity-60')
  })

  it('still names and describes a choice whose value has a space in it', () => {
    render(<ChoiceList multiple label="Pick" choices={[{ value: 'Main generator', label: 'Generator', details: 'The big one.' }]} value={[]} onChange={() => {}} />)
    expect(screen.getByRole('checkbox', { name: 'Generator' })).toHaveAccessibleDescription('The big one.')
  })

  it('describes a selected choice with its selected content as well as its details', () => {
    const choices = [{ value: 'a', label: 'Alpha', details: 'Plain.', selectedContent: <p>Off for the boat.</p> }]
    const { rerender } = render(<ChoiceList multiple label="Pick" choices={choices} value={['a']} onChange={() => {}} />)
    expect(screen.getByRole('checkbox', { name: 'Alpha' })).toHaveAccessibleDescription('Plain. Off for the boat.')
    rerender(<ChoiceList multiple label="Pick" choices={choices} value={[]} onChange={() => {}} />)
    expect(screen.getByRole('checkbox', { name: 'Alpha' })).toHaveAccessibleDescription('Plain.')
  })

  it('prints nothing for a selected content of 0', () => {
    render(<ChoiceList label="Pick" choices={[{ value: 'a', label: 'Alpha', selectedContent: 0 }]} value="a" onChange={() => {}} />)
    expect(screen.getByRole('group', { name: 'Pick' })).not.toHaveTextContent('0')
  })

  it('shows secondary content whether or not the choice is selected, and clicking it does not toggle', () => {
    const onChange = vi.fn()
    const choices = [{ value: 'a', label: 'Alpha', secondaryContent: <button type="button">Extra</button> }]
    const { rerender } = render(<ChoiceList multiple label="Pick" choices={choices} value={[]} onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: 'Extra' }))
    expect(onChange).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Extra' }).parentElement).toHaveClass('pl-7')
    rerender(<ChoiceList multiple label="Pick" choices={choices} value={['a']} onChange={onChange} />)
    expect(screen.getByRole('button', { name: 'Extra' })).toBeInTheDocument()
  })

  it('can hide its label from sight and still name the group', () => {
    render(<ChoiceList labelHidden label="Pick" choices={CHOICES} value="a" onChange={() => {}} />)
    expect(screen.getByRole('group', { name: 'Pick' })).toBeInTheDocument()
    expect(screen.getByText('Pick')).toHaveClass('sr-only')
  })

  it('puts the error in an alert before the choices', () => {
    render(<ChoiceList label="Pick" error="Could not read." choices={CHOICES} value="a" onChange={() => {}} />)
    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent('Could not read.')
    expect(alert).toHaveClass('text-xs', 'text-destructive')
    const first = screen.getByRole('radio', { name: 'Alpha' })
    expect(alert.compareDocumentPosition(first) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('puts the list details inside the group, after the choices', () => {
    render(<ChoiceList label="Pick" details="A footnote." choices={CHOICES} value="a" onChange={() => {}} />)
    const group = screen.getByRole('group', { name: 'Pick' })
    const note = within(group).getByText('A footnote.')
    expect(note).toHaveClass('text-xs', 'text-muted-foreground')
    const last = within(group).getByRole('radio', { name: 'Charlie' })
    expect(last.compareDocumentPosition(note) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('single: allows nothing selected and reports a click', () => {
    const onChange = vi.fn()
    render(<ChoiceList label="Pick" choices={CHOICES} value={undefined} onChange={onChange} />)
    for (const radio of screen.getAllByRole('radio')) expect(radio).not.toBeChecked()
    fireEvent.click(screen.getByRole('radio', { name: 'Bravo' }))
    expect(onChange).toHaveBeenCalledWith('b')
  })

  it('merges listClassName onto the list wrapper', () => {
    render(<ChoiceList label="Pick" listClassName="grid grid-cols-2" choices={CHOICES} value="a" onChange={() => {}} />)
    expect(screen.getByRole('group', { name: 'Pick' }).querySelector('[data-slot="choice-list-rows"]')).toHaveClass('grid', 'grid-cols-2')
  })

  it('keeps the control on the first line when a label with no details wraps', () => {
    render(<ChoiceList label="Pick" choices={CHOICES} value="a" onChange={() => {}} />)
    const radio = screen.getByRole('radio', { name: 'Alpha' })
    expect(radio.parentElement).toHaveClass('items-start')
    expect(radio).toHaveClass('mt-3.5')
    expect(screen.getByText('Alpha').closest('label')).toHaveClass('py-3')
  })

  it('shows empty only when there are no choices', () => {
    const { rerender } = render(<ChoiceList label="Pick" empty={<p>Nothing here.</p>} choices={CHOICES} value="a" onChange={() => {}} />)
    expect(screen.queryByText('Nothing here.')).not.toBeInTheDocument()
    rerender(<ChoiceList label="Pick" empty={<p>Nothing here.</p>} choices={[]} value="" onChange={() => {}} />)
    expect(screen.getByText('Nothing here.')).toBeInTheDocument()
    expect(screen.queryByRole('radio')).not.toBeInTheDocument()
  })
})
