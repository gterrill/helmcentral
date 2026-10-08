import { Folder } from 'lucide-react'
import { useState } from 'react'

import { Breadcrumb, BreadcrumbItem, BreadcrumbLink, BreadcrumbList, BreadcrumbSeparator } from '@/components/ui/breadcrumb'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useDocuments } from '@/hooks/use-documents'

// The Move dialog shared by the Documents listing and a document's Details
// page, so both move into a folder the same way.

/** A one-level-at-a-time folder browser embedded in the Move dialog. It
 * reuses useDocuments the same way the panel itself does - the picker is
 * just another folder view, and moves want the operator to be able to
 * descend into a destination the same way browsing does. */
function FolderPicker({
  onPick,
  onFolderCreated,
}: {
  onPick: (folderId: string | null) => void
  /** Review finding: the picker owns its own useDocuments(pickerFolderId)
   * instance, entirely separate from the panel's own - so a folder created
   * here used to refresh only the picker's view. Cancel the dialog and the
   * panel's own listing had never heard of it: missing from the table, and
   * a second attempt from the toolbar's New folder button hit a 409 for a
   * folder the operator couldn't see. This just tells the caller a folder
   * was created; DocumentsPanel below refreshes its own listing in
   * response, leaving the picker's own descend-into-it behaviour (below)
   * unchanged. */
  onFolderCreated: () => void
}) {
  const [pickerFolderId, setPickerFolderId] = useState<string | null>(null)
  const picker = useDocuments(pickerFolderId)

  // ADR 0115 §6: create-here, so a move no longer has to be
  // abandoned to go make the destination first. Goes through the picker's
  // own createFolder (same useDocuments instance as the browser above), so
  // the new folder shows up in whatever listing this picker is already
  // rendering - then descends into it (setPickerFolderId) so "Move here"
  // immediately means the folder just made, rather than leaving the
  // operator to notice it in the list and click it themselves.
  const [newFolderName, setNewFolderName] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const submitCreate = async () => {
    const name = newFolderName.trim()
    if (name === '' || creating) return
    setCreating(true)
    setCreateError(null)
    try {
      const created = await picker.createFolder(name, pickerFolderId)
      setPickerFolderId(created.id)
      setNewFolderName('')
      onFolderCreated()
    } catch (err) {
      // AGENTS.md fallback policy: the server's own message (e.g. the 409
      // "folder name already exists") rather than an invented one, and the
      // picker stays right where it was - the operator didn't ask to move
      // anywhere, only to create a folder that turned out to already exist.
      setCreateError(err instanceof Error ? err.message : String(err))
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <Breadcrumb>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); setPickerFolderId(null) }}>
              Documents
            </BreadcrumbLink>
          </BreadcrumbItem>
          {picker.path.map((folder) => (
            <span key={folder.id} className="contents">
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); setPickerFolderId(folder.id) }}>
                  {folder.name}
                </BreadcrumbLink>
              </BreadcrumbItem>
            </span>
          ))}
        </BreadcrumbList>
      </Breadcrumb>
      <ScrollArea className="h-48 rounded-md border">
        <div className="flex flex-col p-1">
          {picker.folders.length === 0 && (
            <p className="p-3 text-sm text-muted-foreground">No subfolders here.</p>
          )}
          {picker.folders.map((folder) => (
            <button
              key={folder.id}
              type="button"
              onClick={() => setPickerFolderId(folder.id)}
              className="flex items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm hover:bg-accent"
            >
              <Folder className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
              {folder.name}
            </button>
          ))}
        </div>
      </ScrollArea>
      <div className="flex items-center gap-2">
        <Input
          aria-label="New folder name"
          placeholder="New folder name"
          value={newFolderName}
          onChange={(e) => setNewFolderName(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void submitCreate() } }}
          className="flex-1"
        />
        <Button type="button" variant="outline" disabled={newFolderName.trim() === '' || creating} onClick={() => { void submitCreate() }}>
          Create folder
        </Button>
      </div>
      {createError && <p role="alert" className="text-sm text-destructive">{createError}</p>}
      <Button type="button" onClick={() => onPick(pickerFolderId)}>Move here</Button>
    </div>
  )
}

export function MoveToFolderDialog({
  open,
  onOpenChange,
  label,
  onPick,
  onFolderCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** What is being moved - a document title, a folder name, "3 document(s)". */
  label: string
  onPick: (folderId: string | null) => void
  onFolderCreated: () => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Move {label}</DialogTitle>
          <DialogDescription>Pick the destination folder.</DialogDescription>
        </DialogHeader>
        {open && <FolderPicker onPick={onPick} onFolderCreated={onFolderCreated} />}
      </DialogContent>
    </Dialog>
  )
}
