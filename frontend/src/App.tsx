import { Compass, Sailboat, Waves } from 'lucide-react'

import { MarineHeader } from '@/components/marine-header'
import { Button } from '@/components/ui/button'

export function App() {
  return (
    <div className="min-h-screen p-4 md:p-6">
      <div className="mx-auto flex max-w-[1800px] flex-col gap-4">
        <MarineHeader />

        <div className="grid gap-4 rounded-xl border bg-card/80 p-4 shadow-sm backdrop-blur-sm md:grid-cols-[260px_1fr_360px]">
        <aside className="space-y-4">
          <section className="rounded-lg border bg-card p-4">
            <h2 className="font-display text-xs tracking-[0.22em] text-muted-foreground">Depth</h2>
            <p className="mt-2 font-display text-6xl text-secondary">6.3</p>
            <p className="text-sm text-muted-foreground">ft under keel</p>
          </section>
          <section className="rounded-lg border bg-card p-4">
            <h2 className="font-display text-xs tracking-[0.22em] text-muted-foreground">Position</h2>
            <p className="mt-2 font-mono text-sm">25° 29' 10.2\" N</p>
            <p className="font-mono text-sm">76° 38' 14.0\" W</p>
            <div className="mt-3 rounded-md bg-secondary/10 px-3 py-2 font-display text-2xl text-secondary">63° ENE</div>
          </section>
        </aside>

        <main className="rounded-lg border bg-card p-4">
          <div className="mb-3 flex items-center justify-between">
            <h1 className="font-display text-sm tracking-[0.24em] text-muted-foreground">Apparent Wind - Course Up</h1>
            <Button variant="outline" size="sm">
              <Compass className="h-4 w-4" />
              AWA 061°
            </Button>
          </div>
          <div className="grid place-items-center rounded-xl border bg-background/70 p-8">
            <div className="relative h-[420px] w-[420px] rounded-full border-2 border-border bg-card shadow-inner">
              <div className="absolute inset-0 grid place-items-center">
                <div className="text-center">
                  <p className="font-display text-3xl text-primary">PORT 2°</p>
                  <p className="font-display text-9xl leading-none text-primary">13</p>
                  <p className="font-display text-4xl text-muted-foreground">kts</p>
                </div>
              </div>
              <div className="absolute left-1/2 top-8 h-[160px] w-[2px] -translate-x-1/2 bg-secondary" />
            </div>
          </div>
        </main>

        <aside className="space-y-4">
          <section className="rounded-lg border bg-card p-4">
            <h2 className="font-display text-xs tracking-[0.22em] text-muted-foreground">Battery & Power</h2>
            <div className="mt-2 flex items-end gap-2">
              <span className="font-display text-7xl text-primary">68</span>
              <span className="pb-2 text-3xl">%</span>
            </div>
            <p className="text-secondary">+24.8A / +663W</p>
            <div className="mt-4 space-y-2 text-sm">
              <div className="flex justify-between rounded-md bg-muted/50 px-3 py-2">
                <span>Solar Output</span>
                <span className="font-semibold">1868W</span>
              </div>
              <div className="flex justify-between rounded-md bg-muted/50 px-3 py-2">
                <span>AC Output</span>
                <span className="font-semibold">1017W</span>
              </div>
            </div>
          </section>
          <section className="rounded-lg border bg-card p-4">
            <h2 className="font-display text-xs tracking-[0.22em] text-muted-foreground">Actions</h2>
            <div className="mt-3 grid gap-2">
              <Button>
                <Sailboat className="h-4 w-4" />
                Set Anchor
              </Button>
              <Button variant="secondary">
                <Waves className="h-4 w-4" />
                Drop Here
              </Button>
            </div>
          </section>
        </aside>
        </div>
      </div>
    </div>
  )
}
