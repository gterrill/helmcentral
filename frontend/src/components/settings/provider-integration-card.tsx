import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'

interface ProviderIntegrationCardProps {
  id: string
  name: string
  description: string
  active: boolean
  onActivate: (id: string) => void
  onOpenSettings: (id: string) => void
}

/**
 * One provider's row inside a Plugins section's ProviderGroup list: name,
 * description, a Settings button that always renders (so an
 * inactive provider's host/secret config can still be reviewed or edited
 * ahead of activating it), and an activate Switch. The active card's switch
 * is checked+disabled — there is no explicit "deactivate" affordance,
 * activating a different card in the group is the only way to change which
 * one is active.
 */
export function ProviderIntegrationCard({
  id,
  name,
  description,
  active,
  onActivate,
  onOpenSettings,
}: ProviderIntegrationCardProps) {
  return (
    <li className="flex min-w-0 flex-col gap-3 py-3 sm:flex-row sm:items-center sm:gap-4">
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-semibold text-foreground">{name}</p>
        <p className="line-clamp-2 text-xs text-muted-foreground">{description}</p>
      </div>

      <div className="flex shrink-0 items-center justify-between gap-4 sm:justify-end">
        <Button
          type="button"
          variant="outline"
          className="h-8 whitespace-nowrap px-3 text-[10px] uppercase tracking-[0.1em]"
          onClick={() => onOpenSettings(id)}
        >
          Settings
        </Button>

        <div className="flex items-center gap-2">
          <span className="w-14 text-right text-[10px] uppercase tracking-[0.1em] text-muted-foreground">
            {active ? 'Active' : 'Inactive'}
          </span>
          <Switch
            checked={active}
            disabled={active}
            onCheckedChange={(checked) => {
              if (checked) onActivate(id)
            }}
            aria-label={`Activate ${name}`}
          />
        </div>
      </div>
    </li>
  )
}
