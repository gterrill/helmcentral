import { CircleCheck, Info, LoaderCircle, OctagonX, TriangleAlert } from 'lucide-react'
import { Toaster as Sonner } from 'sonner'

type SonnerProps = React.ComponentProps<typeof Sonner>

export interface ToasterProps extends Omit<SonnerProps, 'theme'> {
  /**
   * Upstream shadcn reads this from next-themes. This app has no theme
   * provider — useDarkMode toggles a `dark` class on <html> directly — so the
   * caller passes the flag down instead, as App does for its other themed
   * children. Keeps next-themes out of the dependency tree entirely.
   */
  isDarkTheme?: boolean
}

const Toaster = ({ isDarkTheme = false, ...props }: ToasterProps) => (
  <Sonner
    theme={isDarkTheme ? 'dark' : 'light'}
    className="toaster group"
    icons={{
      success: <CircleCheck className="size-4" />,
      info: <Info className="size-4" />,
      warning: <TriangleAlert className="size-4" />,
      error: <OctagonX className="size-4" />,
      loading: <LoaderCircle className="size-4 animate-spin" />,
    }}
    toastOptions={{
      classNames: {
        toast:
          'group toast group-[.toaster]:bg-background group-[.toaster]:text-foreground group-[.toaster]:border-border group-[.toaster]:shadow-lg',
        description: 'group-[.toast]:text-muted-foreground',
        actionButton: 'group-[.toast]:bg-primary group-[.toast]:text-primary-foreground',
        cancelButton: 'group-[.toast]:bg-muted group-[.toast]:text-muted-foreground',
      },
    }}
    {...props}
  />
)

export { Toaster }
