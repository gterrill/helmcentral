# ADR 0170: The Documents page navigates folders through a tree pane

## Status

Accepted (2026-10-12). Extends [ADR 0142](0142-crud-pattern-library.md) with a tree
layout. Changes the navigation described in [ADR 0106](0106-documents-in-the-binary.md).

## Context

The Documents page mixed three navigation ideas. Subfolders were rows at the top of the
same table as the documents, under a "Folders" group header, so a folder with forty
documents pushed its own subfolders out of the way of the sort. A breadcrumb above the
title was the only way back up, one level per click, and the only way to reach a distant
folder was to walk down to it. "Unfiled notes (N)" was a toggle button in the toolbar,
a saved view posing as a filter.

At the nav station the cost is the clicks. Filing a receipt into `Receipts/2026` from the
root took three clicks to get there and the Move dialog to file it, and the operator
could not see the shape of the library at any point.

## Decision

1. **A persistent tree pane on a wide screen.** `TreeLayout` is a new pattern in
   `components/patterns`: a `w-56` pane beside the content, which can be hidden (remembered
   in localStorage). Below the mobile breakpoint the pane is not on the page; a "Folders"
   button opens it in a left Sheet, and the Sheet closes when a folder is chosen or a
   folder action is picked.
2. **`FolderTree` renders the pane.** WAI-ARIA tree: `role="tree"`/`treeitem`,
   `aria-expanded`, `aria-selected`, `aria-level`, a roving tabindex, arrow keys, Home and
   End, Enter to select. Counts are right-aligned and muted, names truncate. Expansion is
   remembered per device in localStorage, every access in try/catch. The ancestors of the
   selected folder are always opened.
3. **The whole tree comes from one endpoint.** `GET /api/document-folders/tree` returns
   every folder flat, with `parent_id`, `is_manual` (from `document_folders.role`) and
   `document_count` (documents filed directly in it). The panel builds the hierarchy.
   The page refreshes it after any folder create, rename, move, delete or manual toggle,
   and after document moves, uploads and deletes, so the counts stay true.
4. **The pane holds everything that navigates.** "Documents" (the root) at the top, the folders,
   a divider, then "Unfiled notes" with its count as a pseudo-node that switches the page
   to the Unfiled notes view. The toolbar button, the breadcrumb and the Folders/Documents
   group rows are removed. The table lists documents only. The page title is the current
   folder, or "Unfiled notes" in that view.
5. **Folder actions move to the node.** Rename, Move…, Treat as a manual or Stop treating
   as manual (top-level folders only) and Delete are a menu on each node, shown on hover or
   focus and always on a touch screen, and opened from the keyboard with Shift+F10 or the
   menu key. They reuse the existing dialogs and handlers. Deleting the folder being
   viewed steps out to its parent.
6. **Dragging files documents.** A table row is draggable; a ticked row drags the whole
   selection. Dropping on a folder node (or on Documents, the root) calls the existing
   move mutation. Move… and the bulk Move to… remain, and are the touch route. An upload
   drag from outside is told apart by `Files` in the drag types, so a row dragged across
   the page does not light up the upload drop zone.
7. **A manual is a leaf in the pane.** A manual is read as one thing in its own view, so
   its sections are not listed as folders. A page opened on a folder inside a manual
   highlights the manual. Its count is the whole subtree: the endpoint returns
   `subtree_document_count` beside `document_count` for every folder, and the pane shows
   the subtree figure for a manual. The tree's `is_manual` is also what the page uses to
   decide that a folder is a manual.

## Alternatives rejected

- **An overlay drawer on a wide screen.** One more click to open it on every folder
  change, on the screen where the operator files things most. Kept for the phone, where
  width is the constraint.
- **Lazy per-level fetching.** Each expand would wait on a request, and a library of a few
  hundred folders is a payload of a few kilobytes. One request, built once, is simpler and
  keeps counts consistent across levels.
- **Keeping the breadcrumb beside the tree.** It repeated what the highlighted node and the
  page title already say.
- **Folder drag-and-drop for reparenting.** Not needed for filing documents, and a
  misplaced drop on a moving boat is costly to find again. Move… stays for folders.

## Consequences

- The Documents page gains one request on load, and one after each change that affects a
  folder or a count.
- A subfolder of a manual cannot be reached from the pane, only from inside the manual.
- `IndexTable` gains optional row-drag props. `TreeLayout` and `FolderTree` appear in the
  dev `/patterns` gallery. The operator-facing description is in `docs/features/documents.md`.
