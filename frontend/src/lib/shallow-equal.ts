/** True when two objects have the same keys and `Object.is` values at each. */
export function shallowEqual(a: object, b: object): boolean {
  const left = a as Record<string, unknown>
  const right = b as Record<string, unknown>
  const keys = Object.keys(left)
  return keys.length === Object.keys(right).length && keys.every(key => key in right && Object.is(left[key], right[key]))
}

/** True when two arrays are the same length and each pair of flat objects is `shallowEqual`. */
export function shallowEqualArray<T extends object>(a: readonly T[], b: readonly T[]): boolean {
  return a === b || (a.length === b.length && a.every((item, i) => shallowEqual(item, b[i])))
}
