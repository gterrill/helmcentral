import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
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
 * One provider's "integration card" inside a Widgets tab's ProviderGroup
 * grid: name, description, a Settings button that always renders (so an
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
    <Card className="gap-3 border-border py-4">
      <CardHeader className="gap-1 px-4">
        <CardTitle className="truncate text-sm font-semibold">{name}</CardTitle>
        <CardDescription className="line-clamp-2 text-[11px] text-muted-foreground">
          {description}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex items-center justify-between gap-2 px-4">
        <Button
          type="button"
          variant="outline"
          className="h-8 whitespace-nowrap px-3 text-[10px] uppercase tracking-[0.1em]"
          onClick={() => onOpenSettings(id)}
        >
          Settings
        </Button>

        <div className="flex items-center gap-2">
          <span className="text-[10px] uppercase tracking-[0.1em] text-muted-foreground">
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
      </CardContent>
    </Card>
  )
}
