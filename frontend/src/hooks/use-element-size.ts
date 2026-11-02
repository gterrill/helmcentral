import { useCallback, useLayoutEffect, useState } from 'react'

export interface ElementSize {
  w: number
  h: number
}

/** The size of an element, kept up to date as it is resized. Attach `ref` to
 * the element. The size is null until the element is there and has been
 * measured, which is after the first render. */
export function useElementSize<T extends HTMLElement>() {
  const [element, setElement] = useState<T | null>(null)
  const [size, setSize] = useState<ElementSize | null>(null)
  const ref = useCallback((el: T | null) => setElement(el), [])

  useLayoutEffect(() => {
    if (!element) {
      setSize(null)
      return
    }
    const box = element.getBoundingClientRect()
    setSize({ w: box.width, h: box.height })
    const observer = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect
      setSize((prev) => (prev && prev.w === width && prev.h === height ? prev : { w: width, h: height }))
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [element])

  return [ref, size] as const
}
