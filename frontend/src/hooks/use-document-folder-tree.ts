import { invalidateQueries, useApiQuery } from '@/lib/api-query'

/** One folder in the whole-library tree: flat, parent by id. */
export interface DocumentFolderTreeNode {
  id: string
  name: string
  parent_id: string | null
  is_manual: boolean
  /** Documents filed directly in this folder, not in its subfolders. */
  document_count: number
  /** document_count plus every document in the folder's descendants. */
  subtree_document_count: number
}

const TREE_PATH = '/api/document-folders/tree'
const NO_FOLDERS: DocumentFolderTreeNode[] = []

// Invalidates rather than refetches: the caller is reporting a change, so a
// read already in flight from before it must not be the one that lands.
async function refreshTree(): Promise<void> {
  invalidateQueries([TREE_PATH])
}

/**
 * Every folder in the library in one request, for the Documents page's
 * folder pane. Writes in use-documents.ts invalidate it; `refresh()` is for
 * the changes made through other hooks (flagging a manual, capturing a
 * note). A failure is surfaced, never swallowed.
 */
export function useDocumentFolderTree() {
  const query = useApiQuery({
    queryKey: [TREE_PATH],
    path: TREE_PATH,
    select: (raw: { folders?: DocumentFolderTreeNode[] }) => raw.folders ?? NO_FOLDERS,
  })
  return {
    folders: query.data ?? NO_FOLDERS,
    loading: query.isLoading,
    error: query.error?.message ?? null,
    refresh: refreshTree,
  }
}
