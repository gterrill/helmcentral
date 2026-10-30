import { useState, type ComponentType } from 'react'

/**
 * An index keeps its filter, search and sort in the address. This holds them
 * the way the route does, so a test drives the real index with the state it
 * would be given.
 */
export function withListState<L, P extends { list: L; onListChange: (list: L) => void }>(
  Index: ComponentType<P>,
  empty: L,
) {
  return function IndexWithListState(props: Omit<P, 'list' | 'onListChange'> & { initial?: L }) {
    const { initial, ...rest } = props
    const [list, setList] = useState<L>(initial ?? empty)
    return <Index {...(rest as unknown as P)} list={list} onListChange={setList} />
  }
}
