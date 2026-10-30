import { describe, expect, it } from 'vitest'

import { flattenManualDocuments, type ManualTreeNode } from '@/hooks/use-manuals'

const node = (type: 'folder' | 'document', id: string, children?: ManualTreeNode[]): ManualTreeNode =>
  ({ type, id, name: id, sort_index: 0, updated_at: '', children })

describe('flattenManualDocuments', () => {
  it('lists the documents depth first in the order the server sent them, leaving the folders out', () => {
    const tree = node('folder', 'm', [node('document', 'a'), node('folder', 'f', [node('document', 'b'), node('document', 'c')]), node('document', 'd')])
    expect(flattenManualDocuments(tree).map((n) => n.id)).toEqual(['a', 'b', 'c', 'd'])
  })
})
