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
import { useTileSave } from '@/hooks/use-tile-save'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  EMBED_TITLE_MAX_LENGTH,
  EMBED_URL_MAX_LENGTH,
  isValidEmbedUrl,
  type DashboardLayoutItem,
  type DashboardWidgetId,
  type EmbedWidgetConfig,
} from '@/lib/dashboard-widgets'
import { identifierInputProps } from '@/lib/identifier-input'

interface EmbedConfigDialogProps {
  widget: DashboardLayoutItem | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (id: DashboardWidgetId, config: EmbedWidgetConfig) => void | Promise<void>
}

export function EmbedConfigDialog({ widget, open, onOpenChange, onSave }: EmbedConfigDialogProps) {
  const [url, setUrl] = useState('')
  const [title, setTitle] = useState('')
  const [frameless, setFrameless] = useState(false)
  const { saving, error: saveError, run } = useTileSave(widget?.id)

  // Re-seed whenever a different widget is opened, so editing one embed never
  // shows another's URL.
  useEffect(() => {
    setUrl(widget?.embed?.url ?? '')
    setTitle(widget?.embed?.title ?? '')
    setFrameless(widget?.embed?.frameless ?? false)
  }, [widget?.id, widget?.embed?.url, widget?.embed?.title, widget?.embed?.frameless])

  if (!widget) return null

  const canSave = isValidEmbedUrl(url)
  // Stay quiet until there is something to complain about — a freshly added
  // embed opens with an empty URL, which is expected, not an error.
  const showUrlError = url.trim() !== '' && !canSave

  // The page closes the dialog once the tile is written. Closing here would
  // also close it over a failed write.
  const handleSave = () => {
    if (!canSave) return
    void run(() => onSave(widget.id, { title: title.trim(), url: url.trim(), frameless }))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Embed</DialogTitle>
          <DialogDescription>
            Show an external page — a Grafana panel, a Node-RED dashboard — inside this tile.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <Field>
            <FieldLabel htmlFor="embed-url">URL</FieldLabel>
            <Input
              id="embed-url"
              type="url"
              {...identifierInputProps}
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
            <FieldDescription>Shown in the tile header. Optional.</FieldDescription>
          </Field>

          <Field orientation="horizontal">
            <Switch id="embed-frameless" checked={frameless} onCheckedChange={setFrameless} />
            <FieldLabel htmlFor="embed-frameless">Frameless</FieldLabel>
          </Field>
          <FieldDescription>
            No title bar or padding — fills the tile. Only applies outside layout editing.
          </FieldDescription>
        </div>

        <DialogFooter>
          {saveError && <p role="alert" className="mr-auto text-sm text-destructive">{saveError}</p>}
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={!canSave || saving}>
            {saving ? 'Saving…' : 'Save'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
