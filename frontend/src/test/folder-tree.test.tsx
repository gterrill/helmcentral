import { describe, it, expect, vi, beforeEach } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'

import { Route } from 'lucide-react'

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
    // Vertical padding on a clamped box shows the top of the hidden third line.
    expect(name).toHaveClass('my-1.5')
    expect(name).not.toHaveClass('py-1.5')
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

// No SidebarProvider: the pane must render anywhere, including the dev gallery.
function renderLayout(props: Partial<React.ComponentProps<typeof TreeLayout>> = {}) {
  return render(
    <TreeLayout pane={<p>the pane</p>} collapsedStorageKey="test.hidden" {...props}><p>content</p></TreeLayout>,
  )
}

describe('TreeLayout', () => {
  it('hides and restores the pane on a wide screen, remembering the choice', () => {
    const { unmount } = renderLayout()
    expect(screen.getByText('the pane')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Hide folders' }))
    expect(screen.queryByText('the pane')).not.toBeInTheDocument()
    expect(localStorage.getItem('test.hidden')).toBe('1')
    unmount()

    renderLayout()
    expect(screen.queryByText('the pane')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Show folders' }))
    expect(screen.getByText('the pane')).toBeInTheDocument()
    expect(screen.getByText('content')).toBeInTheDocument()
    expect(localStorage.getItem('test.hidden')).toBe('0')
  })

  it('holds the pane at the sidebar width, whatever the longest name asks for', () => {
    renderLayout()
    const wrapper = screen.getByRole('complementary', { name: 'Folders' }).parentElement
    expect(wrapper).toHaveClass('w-64', 'shrink-0')
  })

  it('collapses to a slim rail beside the content rather than a row above it', () => {
    renderLayout()
    fireEvent.click(screen.getByRole('button', { name: 'Hide folders' }))
    const rail = screen.getByRole('button', { name: 'Show folders' })
    const strip = rail.parentElement
    expect(strip).toHaveClass('w-10', 'rounded-xl', 'bg-sidebar')
    expect(rail).not.toHaveTextContent('Show folders')
    expect(strip?.nextElementSibling).toContainElement(screen.getByText('content'))
  })

  it('does not reopen the pane from a tap on the empty part of the strip', () => {
    renderLayout()
    fireEvent.click(screen.getByRole('button', { name: 'Hide folders' }))
    fireEvent.click(screen.getByRole('button', { name: 'Show folders' }).parentElement!)
    expect(screen.queryByText('the pane')).not.toBeInTheDocument()
  })

  it('names both controls in a title as well as an aria-label', () => {
    renderLayout()
    const hide = screen.getByRole('button', { name: 'Hide folders' })
    expect(hide).toHaveAttribute('title', 'Hide folders')
    expect(hide).toHaveClass('h-10', 'w-10')
    fireEvent.click(hide)
    expect(screen.getByRole('button', { name: 'Show folders' })).toHaveAttribute('title', 'Show folders')
  })

  it('ties the hide button to the pane content; the rail has no target to control', () => {
    renderLayout()
    const hide = screen.getByRole('button', { name: 'Hide folders' })
    expect(hide).toHaveAttribute('aria-expanded', 'true')
    const controls = hide.getAttribute('aria-controls')
    expect(controls).toBeTruthy()
    expect(document.getElementById(controls!)).toContainElement(screen.getByText('the pane'))

    fireEvent.click(hide)
    const rail = screen.getByRole('button', { name: 'Show folders' })
    expect(rail).toHaveAttribute('aria-expanded', 'false')
    expect(rail).not.toHaveAttribute('aria-controls')
  })

  it('shows the pane icon in the header and on the rail, Folder by default', () => {
    renderLayout()
    expect(document.querySelector('[data-sidebar="header"] svg.lucide-folder')).not.toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Hide folders' }))
    expect(screen.getByRole('button', { name: 'Show folders' }).querySelector('svg.lucide-folder')).not.toBeNull()
  })

  it('shows a custom pane icon in the header and on the rail', () => {
    renderLayout({ paneIcon: Route, paneLabel: 'Cruises' })
    expect(document.querySelector('[data-sidebar="header"] svg.lucide-route')).not.toBeNull()
    expect(document.querySelector('svg.lucide-folder')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Hide cruises' }))
    expect(screen.getByRole('button', { name: 'Show cruises' }).querySelector('svg.lucide-route')).not.toBeNull()
  })
})
