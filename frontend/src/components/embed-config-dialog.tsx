import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  EMBED_TITLE_MAX_LENGTH,
  EMBED_URL_MAX_LENGTH,
  isValidEmbedUrl,
  type DashboardLayoutItem,
  type DashboardWidgetId,
  type EmbedWidgetConfig,
} from '@/lib/dashboard-widgets'

interface EmbedConfigDialogProps {
  widget: DashboardLayoutItem | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (id: DashboardWidgetId, config: EmbedWidgetConfig) => void
}

export function EmbedConfigDialog({ widget, open, onOpenChange, onSave }: EmbedConfigDialogProps) {
  const [url, setUrl] = useState('')
  const [title, setTitle] = useState('')

  // Re-seed whenever a different widget is opened, so editing one embed never
  // shows another's URL.
  useEffect(() => {
    setUrl(widget?.embed?.url ?? '')
    setTitle(widget?.embed?.title ?? '')
  }, [widget?.id, widget?.embed?.url, widget?.embed?.title])

  if (!widget) return null

  const canSave = isValidEmbedUrl(url)
  // Stay quiet until there is something to complain about — a freshly added
  // embed opens with an empty URL, which is expected, not an error.
  const showUrlError = url.trim() !== '' && !canSave

  const handleSave = () => {
    if (!canSave) return
    onSave(widget.id, { title: title.trim(), url: url.trim() })
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Embed</DialogTitle>
          <DialogDescription>
            Show an external page — a Grafana panel, a Node-RED dashboard — inside this widget.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <Field>
            <FieldLabel htmlFor="embed-url">URL</FieldLabel>
            <Input
              id="embed-url"
              value={url}
              maxLength={EMBED_URL_MAX_LENGTH}
              placeholder="http://boat.local:3000/d-solo/abc?panelId=2&kiosk"
              onChange={(event) => setUrl(event.target.value)}
            />
            {showUrlError ? (
              <FieldError>Address must be an http:// or https:// URL.</FieldError>
            ) : (
              <FieldDescription>
                In Grafana use Share → Embed, and add <code>&amp;kiosk</code> to drop the panel
                chrome. An http:// address is blocked as mixed content if Helmcentral itself is
                ever served over https.
              </FieldDescription>
            )}
          </Field>

          <Field>
            <FieldLabel htmlFor="embed-title">Title</FieldLabel>
            <Input
              id="embed-title"
              value={title}
              maxLength={EMBED_TITLE_MAX_LENGTH}
              placeholder="Windrose"
              onChange={(event) => setTitle(event.target.value)}
            />
            <FieldDescription>Shown in the widget header. Optional.</FieldDescription>
          </Field>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={!canSave}>
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
