/** Saves a blob as a file through a synthetic link click. The link is attached
 * so every browser honours the click, and the object URL is released after
 * the download has had time to start, not before. */
export function saveBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.style.display = 'none'
  document.body.appendChild(link)
  try {
    link.click()
  } finally {
    link.remove()
    setTimeout(() => URL.revokeObjectURL(url), 10_000)
  }
}
