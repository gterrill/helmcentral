import { ChevronRight, MoreVertical } from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type DragEvent, type KeyboardEvent, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

import type { RowAction } from './index-table'

// ADR 0170: the tree half of TreeLayout. A navigation tree for a record
// surface (the Documents page's folders): nested nodes with chevrons, one
// selected node, counts, a per-node action menu, and optional drop targets.
// Keyboard model follows the WAI-ARIA tree pattern (roving tabindex, arrows,
// Home/End, Enter to select). Expansion is remembered per `storageKey`.

export interface TreeNode {
  id: string
  label: string
  icon?: ReactNode
  /** Right-aligned, muted. Omit to show nothing. */
  count?: number
  children?: TreeNode[]
  /** The per-node "..." menu. */
  actions?: RowAction<TreeNode>[]
  /** Accepts a drop of the tree's `dropType`. Default false. */
  droppable?: boolean
  /** Draws a separator line above this top-level node. */
  separatorBefore?: boolean
}

export interface FolderTreeProps {
  label: string
  nodes: TreeNode[]
  selectedId: string | null
  onSelect: (id: string) => void
  /** localStorage key holding the expanded node ids. */
  storageKey: string
  /** The DataTransfer type a drag must carry to be dropped on a node. */
  dropType?: string
  onDropOnNode?: (nodeId: string, data: string) => void
  /** Called after a selection or a menu action, so a hosting sheet can close. */
  onDismiss?: () => void
  /** Rendered in place of the tree when there are no nodes. */
  empty?: ReactNode
}

function loadExpanded(key: string): Set<string> {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return new Set()
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return new Set()
    return new Set(parsed.filter((v): v is string => typeof v === 'string'))
  } catch {
    return new Set()
  }
}

function saveExpanded(key: string, ids: Set<string>): void {
  try {
    localStorage.setItem(key, JSON.stringify([...ids]))
  } catch {
    // Remembering which nodes are open is a convenience only.
  }
}

/** Ids from the top-level node down to (not including) `targetId`, or null
 * when the node is not in the tree. */
function ancestorsOf(nodes: TreeNode[], targetId: string, trail: string[] = []): string[] | null {
  for (const node of nodes) {
    if (node.id === targetId) return trail
    if (node.children) {
      const found = ancestorsOf(node.children, targetId, [...trail, node.id])
      if (found) return found
    }
  }
  return null
}

interface VisibleNode {
  node: TreeNode
  level: number
  parentId: string | null
  setSize: number
  position: number
}

function flatten(nodes: TreeNode[], expanded: Set<string>, level = 1, parentId: string | null = null): VisibleNode[] {
  const out: VisibleNode[] = []
  nodes.forEach((node, index) => {
    out.push({ node, level, parentId, setSize: nodes.length, position: index + 1 })
    if (node.children && node.children.length > 0 && expanded.has(node.id)) {
      out.push(...flatten(node.children, expanded, level + 1, node.id))
    }
  })
  return out
}

export function FolderTree({
  label,
  nodes,
  selectedId,
  onSelect,
  storageKey,
  dropType,
  onDropOnNode,
  onDismiss,
  empty,
}: FolderTreeProps) {
  const [expanded, setExpanded] = useState<Set<string>>(() => loadExpanded(storageKey))
  const [focusId, setFocusId] = useState<string | null>(null)
  const [menuFor, setMenuFor] = useState<string | null>(null)
  const [dropTarget, setDropTarget] = useState<string | null>(null)
  const containerRef = useRef<HTMLDivElement | null>(null)

  const updateExpanded = (next: Set<string>) => {
    setExpanded(next)
    saveExpanded(storageKey, next)
  }

  // The selected node's ancestors are opened when the selection (or where it
  // sits in the tree) changes. Not when `expanded` changes: collapsing an
  // ancestor by hand must stick.
  const expandedRef = useRef(expanded)
  expandedRef.current = expanded
  const trail = selectedId === null ? null : ancestorsOf(nodes, selectedId)
  const trailKey = trail ? trail.join('\u0000') : null
  useEffect(() => {
    if (trailKey === null || trailKey === '') return
    const ids = trailKey.split('\u0000')
    if (ids.every((id) => expandedRef.current.has(id))) return
    const next = new Set(expandedRef.current)
    for (const id of ids) next.add(id)
    setExpanded(next)
    saveExpanded(storageKey, next)
  }, [selectedId, trailKey, storageKey])

  const visible = useMemo(() => flatten(nodes, expanded), [nodes, expanded])
  const activeId =
    focusId !== null && visible.some((v) => v.node.id === focusId)
      ? focusId
      : selectedId !== null && visible.some((v) => v.node.id === selectedId)
        ? selectedId
        : (visible[0]?.node.id ?? null)

  const focusNode = (id: string) => {
    setFocusId(id)
    containerRef.current?.querySelector<HTMLElement>(`[data-node-id="${CSS.escape(id)}"]`)?.focus()
  }

  const toggle = (id: string) => {
    const next = new Set(expanded)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    updateExpanded(next)
  }

  const select = (id: string) => {
    onSelect(id)
    onDismiss?.()
  }

  const handleKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const row = (e.target as HTMLElement).closest<HTMLElement>('[role="treeitem"]')
    if (!row || e.target !== row) return
    const id = row.dataset.nodeId
    const index = visible.findIndex((v) => v.node.id === id)
    if (index < 0) return
    const current = visible[index]
    const hasChildren = (current.node.children?.length ?? 0) > 0
    const isOpen = expanded.has(current.node.id)

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        if (index < visible.length - 1) focusNode(visible[index + 1].node.id)
        break
      case 'ArrowUp':
        e.preventDefault()
        if (index > 0) focusNode(visible[index - 1].node.id)
        break
      case 'Home':
        e.preventDefault()
        focusNode(visible[0].node.id)
        break
      case 'End':
        e.preventDefault()
        focusNode(visible[visible.length - 1].node.id)
        break
      case 'ArrowRight':
        e.preventDefault()
        if (hasChildren && !isOpen) toggle(current.node.id)
        else if (hasChildren && isOpen) focusNode(visible[index + 1].node.id)
        break
      case 'ArrowLeft':
        e.preventDefault()
        if (hasChildren && isOpen) toggle(current.node.id)
        else if (current.parentId) focusNode(current.parentId)
        break
      case 'Enter':
      case ' ':
        e.preventDefault()
        select(current.node.id)
        break
      case 'ContextMenu':
        if (current.node.actions && current.node.actions.length > 0) {
          e.preventDefault()
          setMenuFor(current.node.id)
        }
        break
      case 'F10':
        if (e.shiftKey && current.node.actions && current.node.actions.length > 0) {
          e.preventDefault()
          setMenuFor(current.node.id)
        }
        break
    }
  }

  const acceptsDrag = (e: DragEvent<HTMLElement>, node: TreeNode) =>
    !!dropType && !!node.droppable && Array.from(e.dataTransfer.types).includes(dropType)

  if (nodes.length === 0) return <>{empty ?? null}</>

  return (
    <div
      ref={containerRef}
      role="tree"
      aria-label={label}
      className="flex flex-col gap-0.5"
      onKeyDown={handleKeyDown}
    >
      {visible.map(({ node, level, setSize, position }) => {
        const hasChildren = (node.children?.length ?? 0) > 0
        const isOpen = expanded.has(node.id)
        const isSelected = node.id === selectedId
        const actions = node.actions ?? []
        return (
          <div key={node.id} className="contents">
            {node.separatorBefore && <div role="separator" className="my-1 border-t border-border" />}
            <div
              role="treeitem"
              data-node-id={node.id}
              aria-level={level}
              aria-setsize={setSize}
              aria-posinset={position}
              aria-selected={isSelected}
              aria-expanded={hasChildren ? isOpen : undefined}
              tabIndex={node.id === activeId ? 0 : -1}
              onClick={() => select(node.id)}
              onFocus={(e) => { if (e.target === e.currentTarget) setFocusId(node.id) }}
              onDragOver={(e) => {
                if (!acceptsDrag(e, node)) return
                e.preventDefault()
                e.dataTransfer.dropEffect = 'move'
                setDropTarget(node.id)
              }}
              onDragLeave={(e) => {
                // Crossing onto a child element of this row is not leaving it.
                if (e.relatedTarget instanceof Node && e.currentTarget.contains(e.relatedTarget)) return
                setDropTarget((prev) => (prev === node.id ? null : prev))
              }}
              onDrop={(e) => {
                if (!acceptsDrag(e, node)) return
                e.preventDefault()
                setDropTarget(null)
                onDropOnNode?.(node.id, e.dataTransfer.getData(dropType!))
              }}
              style={{ paddingLeft: `${(level - 1) * 12 + 4}px` }}
              className={cn(
                'group flex min-h-9 cursor-pointer items-center gap-1 rounded-md pr-1 text-sm text-foreground outline-hidden',
                'hover:bg-accent/60 focus-visible:ring-2 focus-visible:ring-ring',
                isSelected && 'bg-accent font-medium',
                dropTarget === node.id && 'bg-primary/10 ring-2 ring-primary',
              )}
            >
              {hasChildren ? (
                <span
                  aria-hidden="true"
                  data-slot="tree-chevron"
                  className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-sm text-muted-foreground hover:bg-accent"
                  onClick={(e) => { e.stopPropagation(); toggle(node.id) }}
                >
                  <ChevronRight className={cn('h-4 w-4 transition-transform', isOpen && 'rotate-90')} />
                </span>
              ) : (
                <span aria-hidden="true" className="inline-block h-6 w-6 shrink-0" />
              )}
              {node.icon}
              <span className="min-w-0 flex-1 truncate">{node.label}</span>
              {node.count !== undefined && (
                <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{node.count}</span>
              )}
              {actions.length > 0 && (
                <div
                  className="inline-flex shrink-0"
                  onClick={(e) => e.stopPropagation()}
                  onKeyDown={(e) => e.stopPropagation()}
                >
                  <DropdownMenu open={menuFor === node.id} onOpenChange={(open) => setMenuFor(open ? node.id : null)}>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-sm"
                          tabIndex={-1}
                          aria-label={`Actions for ${node.label}`}
                          className="opacity-0 focus-visible:opacity-100 group-hover:opacity-100 group-focus-within:opacity-100 data-popup-open:opacity-100 [@media(pointer:coarse)]:opacity-100"
                        >
                          <MoreVertical className="h-4 w-4" aria-hidden="true" />
                        </Button>
                      }
                    />
                    <DropdownMenuContent align="end">
                      {actions.map((action) => (
                        <DropdownMenuItem
                          key={action.label}
                          variant={action.destructive ? 'destructive' : 'default'}
                          disabled={action.disabled?.(node)}
                          onClick={() => { action.onSelect(node); onDismiss?.() }}
                        >
                          {action.icon}
                          {action.label}
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              )}
            </div>
          </div>
        )
      })}
    </div>
  )
}
