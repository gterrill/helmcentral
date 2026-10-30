import type { PagerProps } from '@/components/patterns'
import { neighbours } from '@/lib/list-order'

/**
 * The pager for the record `id` in an ordered list: the record before it and
 * the one after, named for the button, each opening through `open`. Empty
 * ends are null (a disabled arrow), and so are both when `id` is not in the
 * list.
 */
export function recordPager<T>(
  ordered: readonly T[],
  id: string,
  getId: (record: T) => string,
  label: (record: T) => string,
  open: (id: string) => void,
): PagerProps {
  const around = neighbours(ordered, id, getId)
  const target = (record: T | null) => (record ? { label: label(record), onSelect: () => open(getId(record)) } : null)
  return { previous: target(around.previous), next: target(around.next) }
}
