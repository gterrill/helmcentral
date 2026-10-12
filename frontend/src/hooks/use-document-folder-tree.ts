import { useCallback, useEffect, useRef, useState } from 'react'

import { apiBaseUrl } from '@/config/api'

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

/**
 * Every folder in the library in one request, for the Documents page's
 * folder pane. The caller calls `refresh()` after anything that changes a
 * folder or a count; a failure is surfaced, never swallowed.
 */
export function useDocumentFolderTree() {
  const [folders, setFolders] = useState<DocumentFolderTreeNode[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  // A slow earlier response must not overwrite a newer one.
  const seqRef = useRef(0)

  const refresh = useCallback(async () => {
    const seq = (seqRef.current += 1)
    try {
      const res = await fetch(`${apiBaseUrl}/api/document-folders/tree`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = (await res.json()) as { folders?: DocumentFolderTreeNode[] }
      if (seq !== seqRef.current) return
      setFolders(data.folders ?? [])
      setError(null)
    } catch (err) {
      if (seq !== seqRef.current) return
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === seqRef.current) setLoading(false)
    }
  }, [])

  useEffect(() => { void refresh() }, [refresh])

  return { folders, loading, error, refresh }
}
