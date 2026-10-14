import { describe, it, expect, vi, beforeEach } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'

import { FolderTree, TreeLayout, type TreeNode } from '@/components/patterns'

const nodes: TreeNode[] = [
  { id: 'all', label: 'Documents' },
  {
    id: 'a',
    label: 'Alpha',
    count: 3,
    children: [{ id: 'a1', label: 'Alpha one', count: 1 }, { id: 'a2', label: 'Alpha two' }],
  },
  { id: 'b', label: 'Beta' },
]

function renderTree(props: Partial<React.ComponentProps<typeof FolderTree>> = {}) {
  const onSelect = vi.fn()
  render(<FolderTree label="Folders" nodes={nodes} selectedId="all" onSelect={onSelect} storageKey="test.tree" {...props} />)
  return { onSelect, tree: screen.getByRole('tree', { name: 'Folders' }) }
}

beforeEach(() => {
  try { localStorage.clear() } catch { /* not available */ }
})

describe('FolderTree long names', () => {
  it('wraps a name to two lines and keeps the full name in a title', () => {
    renderTree({ nodes: [{ id: 'long', label: 'Dunk to Townsville', count: 4 }], selectedId: 'long' })
    const name = screen.getByText('Dunk to Townsville')
    expect(name).toHaveClass('line-clamp-2')
    expect(name).not.toHaveClass('truncate')
    expect(name).toHaveAttribute('title', 'Dunk to Townsville')
    expect(screen.getByText('4')).toHaveClass('self-start')
  })
})

describe('FolderTree', () => {
  it('marks the selected node, and only a parent carries aria-expanded', () => {
    renderTree({ selectedId: 'b' })
    expect(screen.getByRole('treeitem', { name: 'Beta' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('treeitem', { name: 'Documents' })).toHaveAttribute('aria-selected', 'false')
    expect(screen.getByRole('treeitem', { name: 'Documents' })).not.toHaveAttribute('aria-expanded')
    expect(screen.getByRole('treeitem', { name: /Alpha/ })).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('treeitem', { name: /Alpha one/ })).not.toBeInTheDocument()
  })

  it('expands the ancestors of the selected node', () => {
    renderTree({ selectedId: 'a2' })
    expect(screen.getByRole('treeitem', { name: /Alpha$|Alpha 3/ })).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('treeitem', { name: 'Alpha two' })).toHaveAttribute('aria-selected', 'true')
  })

  it('keeps an ancestor of the selection collapsed once the operator collapses it', () => {
    renderTree({ selectedId: 'a2' })
    const alpha = screen.getByRole('treeitem', { name: 'Alpha 3' })
    expect(alpha).toHaveAttribute('aria-expanded', 'true')
    fireEvent.click(alpha.querySelector('[data-slot="tree-chevron"]')!)
    expect(screen.getByRole('treeitem', { name: 'Alpha 3' })).toHaveAttribute('aria-expanded', 'false')
  })

  it('remembers which nodes are open', () => {
    const { unmount } = render(<FolderTree label="Folders" nodes={nodes} selectedId="all" onSelect={() => {}} storageKey="remember" />)
    fireEvent.click(screen.getByRole('treeitem', { name: /Alpha/ }).querySelector('[data-slot="tree-chevron"]')!)
    expect(screen.getByRole('treeitem', { name: /Alpha one/ })).toBeInTheDocument()
    unmount()

    render(<FolderTree label="Folders" nodes={nodes} selectedId="all" onSelect={() => {}} storageKey="remember" />)
    expect(screen.getByRole('treeitem', { name: /Alpha one/ })).toBeInTheDocument()
  })

  it('still works when storage throws', () => {
    const get = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('denied') })
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('denied') })
    renderTree()
    fireEvent.click(screen.getByRole('treeitem', { name: /Alpha/ }).querySelector('[data-slot="tree-chevron"]')!)
    expect(screen.getByRole('treeitem', { name: /Alpha one/ })).toBeInTheDocument()
    get.mockRestore()
    set.mockRestore()
  })

  it('clicking the chevron toggles without selecting', () => {
    const { onSelect } = renderTree()
    fireEvent.click(screen.getByRole('treeitem', { name: /Alpha/ }).querySelector('[data-slot="tree-chevron"]')!)
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('arrow keys move focus, expand and collapse, and Enter selects', () => {
    const { onSelect } = renderTree()
    const all = screen.getByRole('treeitem', { name: 'Documents' })
    expect(all).toHaveAttribute('tabindex', '0')
    all.focus()

    fireEvent.keyDown(all, { key: 'ArrowDown' })
    const alpha = screen.getByRole('treeitem', { name: /Alpha/ })
    expect(alpha).toHaveFocus()

    fireEvent.keyDown(alpha, { key: 'ArrowRight' })
    expect(alpha).toHaveAttribute('aria-expanded', 'true')
    fireEvent.keyDown(alpha, { key: 'ArrowRight' })
    const one = screen.getByRole('treeitem', { name: /Alpha one/ })
    expect(one).toHaveFocus()

    fireEvent.keyDown(one, { key: 'ArrowLeft' })
    expect(alpha).toHaveFocus()
    fireEvent.keyDown(alpha, { key: 'ArrowLeft' })
    expect(alpha).toHaveAttribute('aria-expanded', 'false')

    fireEvent.keyDown(alpha, { key: 'End' })
    const beta = screen.getByRole('treeitem', { name: 'Beta' })
    expect(beta).toHaveFocus()
    fireEvent.keyDown(beta, { key: 'Enter' })
    expect(onSelect).toHaveBeenCalledWith('b')
    fireEvent.keyDown(beta, { key: 'ArrowUp' })
    expect(alpha).toHaveFocus()
    fireEvent.keyDown(alpha, { key: 'Home' })
    expect(all).toHaveFocus()
  })

  it('opens a node\'s action menu from the keyboard and runs the action', () => {
    const run = vi.fn()
    renderTree({
      nodes: [{ id: 'x', label: 'Xray', actions: [{ label: 'Rename', onSelect: run }] }],
      selectedId: 'x',
    })
    const item = screen.getByRole('treeitem', { name: 'Xray' })
    item.focus()
    fireEvent.keyDown(item, { key: 'F10', shiftKey: true })
    fireEvent.click(screen.getByRole('menuitem', { name: 'Rename' }))
    expect(run).toHaveBeenCalledTimes(1)
  })

  it('clamps long names to two lines and keeps the count', () => {
    renderTree({ nodes: [{ id: 'x', label: 'A very long folder name', count: 12 }], selectedId: 'x' })
    expect(screen.getByText('A very long folder name').className).toContain('line-clamp-2')
    expect(screen.getByText('12').className).toContain('tabular-nums')
  })
})

describe('TreeLayout', () => {
  it('hides and restores the pane on a wide screen, remembering the choice', () => {
    const { unmount } = render(
      <TreeLayout pane={<p>the pane</p>} collapsedStorageKey="test.hidden"><p>content</p></TreeLayout>,
    )
    expect(screen.getByText('the pane')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Hide folders' }))
    expect(screen.queryByText('the pane')).not.toBeInTheDocument()
    unmount()

    render(<TreeLayout pane={<p>the pane</p>} collapsedStorageKey="test.hidden"><p>content</p></TreeLayout>)
    expect(screen.queryByText('the pane')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Show folders' }))
    expect(screen.getByText('the pane')).toBeInTheDocument()
    expect(screen.getByText('content')).toBeInTheDocument()
  })
})
