import { useRef, type ChangeEvent } from 'react'
import { Camera, ImagePlus } from 'lucide-react'

import { Button } from '@/components/ui/button'

// ADR 0127 (the plan's A5): the equipment editor's photo row - thumbnails
// in order, cover marked, no drag handles (Make cover is the only reorder
// that matters). This component never decides whether a picked file
// uploads immediately or stays local as a Blob - onFilesPicked hands the
// raw files straight to whichever caller mounts it (equipment-editor.tsx
// for a saved item OR a brand new draft; bin-quick-add.tsx for the bin
// page's quick add), which is also where downscaleImage (lib/image-
// downscale.ts) runs, before anything reaches the network. That split is
// what lets the SAME row work for both "each action calls its route
// immediately" (a saved item) and "picked photos stay local until Save"
// (a draft) with no mode flag of its own.

export interface PhotoStripPhoto {
  id: string
  previewUrl: string
  isCover: boolean
}

interface PhotoStripEditorProps {
  photos: PhotoStripPhoto[]
  /** Fired for BOTH Take photo and Add from library, already normalised to
   * a plain array regardless of the input's own `multiple`-ness. */
  onFilesPicked: (files: File[]) => void
  onMakeCover: (id: string) => void
  onRemove: (id: string) => void
  canWrite?: boolean
}

export function PhotoStripEditor({ photos, onFilesPicked, onMakeCover, onRemove, canWrite = true }: PhotoStripEditorProps) {
  const cameraInputRef = useRef<HTMLInputElement>(null)
  const libraryInputRef = useRef<HTMLInputElement>(null)

  const handleInputChange = (e: ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files
    if (files && files.length > 0) onFilesPicked(Array.from(files))
    // Clears the input's own value so pressing Take photo a SECOND time in a
    // row (the bin-quick-add workflow: a front and a back of the same
    // packet) fires a change event even when the browser hands back what
    // looks like "the same" capture - a file input that still holds its
    // previous value never fires 'change' again for an identical pick.
    e.target.value = ''
  }

  return (
    <div className="flex flex-col gap-2">
      {photos.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {photos.map((photo) => (
            <div key={photo.id} className="flex w-20 min-w-0 flex-col items-center gap-1">
              <div className="relative h-20 w-20 shrink-0 overflow-hidden rounded-md border border-border bg-muted">
                <img src={photo.previewUrl} alt="" className="h-full w-full object-cover" />
                {photo.isCover && (
                  <span className="absolute left-1 top-1 rounded-sm bg-background/90 px-1 py-0.5 text-[9px] font-medium uppercase tracking-wider text-primary">
                    Cover
                  </span>
                )}
              </div>
              {canWrite && (
                <div className="flex items-center gap-1.5 text-[10px]">
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-primary disabled:pointer-events-none disabled:opacity-50"
                    disabled={photo.isCover}
                    onClick={() => onMakeCover(photo.id)}
                  >
                    Make cover
                  </button>
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-destructive"
                    onClick={() => onRemove(photo.id)}
                  >
                    Remove
                  </button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {canWrite && (
        <div className="flex items-center gap-2">
          <input
            ref={cameraInputRef}
            type="file"
            accept="image/*"
            capture="environment"
            aria-label="Take photo"
            className="hidden"
            onChange={handleInputChange}
          />
          <input
            ref={libraryInputRef}
            type="file"
            accept="image/*"
            multiple
            aria-label="Add from library"
            className="hidden"
            onChange={handleInputChange}
          />
          <Button type="button" variant="outline" size="sm" className="gap-1.5" onClick={() => cameraInputRef.current?.click()}>
            <Camera className="h-3.5 w-3.5" aria-hidden="true" />
            Take photo
          </Button>
          <Button type="button" variant="outline" size="sm" className="gap-1.5" onClick={() => libraryInputRef.current?.click()}>
            <ImagePlus className="h-3.5 w-3.5" aria-hidden="true" />
            Add from library
          </Button>
        </div>
      )}
    </div>
  )
}
