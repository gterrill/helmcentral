import { useEffect, useState } from 'react'

export interface ImageSize {
  width: number
  height: number
}

/**
 * The natural pixel size of the image at `url`, or null until it has loaded
 * (and for a null url). A load failure leaves it null and reports `failed`,
 * so a caller can say the image would not load instead of drawing at an
 * invented size.
 */
export function useImageSize(url: string | null): { size: ImageSize | null; failed: boolean } {
  const [state, setState] = useState<{ url: string | null; size: ImageSize | null; failed: boolean }>({
    url: null,
    size: null,
    failed: false,
  })

  useEffect(() => {
    if (!url) return
    let cancelled = false
    const img = new Image()
    img.onload = () => {
      if (cancelled) return
      setState({ url, size: { width: img.naturalWidth, height: img.naturalHeight }, failed: false })
    }
    img.onerror = () => {
      if (cancelled) return
      setState({ url, size: null, failed: true })
    }
    img.src = url
    return () => { cancelled = true }
  }, [url])

  if (!url || state.url !== url) return { size: null, failed: false }
  return { size: state.size, failed: state.failed }
}
