import { FolderPickerDialog } from '@/components/documents/folder-picker-dialog'

// The Move dialog shared by the Documents listing and a document's Details
// page, so both move into a folder the same way.
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
  onFolderCreated?: () => void
}) {
  return (
    <FolderPickerDialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Move ${label}`}
      confirmLabel="Move here"
      onPick={onPick}
      onFolderCreated={onFolderCreated}
    />
  )
}
