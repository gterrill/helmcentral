import { useId } from "react";

import type { PlanLegComfort } from "@/lib/plans";

// A leg's comfort (ADR 0302): the server's sentence, the motion line under it, and
// on a tap the hours behind them, each as the server's own line. The words are the
// server's; this only lays them out. Neutral colours throughout: the band is a word,
// never a verdict colour, so it cannot be read as the Go / Marginal / No-Go verdict
// beside it. The row holds the open state, as it does for the route map.

export function PlanComfortChip({ comfort, open, onToggle }: { comfort: PlanLegComfort; open: boolean; onToggle: () => void }) {
  const listId = useId();
  const hasHours = comfort.hours.length > 0;
  return (
    <div data-testid="plan-comfort">
      <button
        type="button"
        className="block w-full min-w-0 text-left"
        aria-expanded={hasHours ? open : undefined}
        aria-controls={hasHours ? listId : undefined}
        disabled={!hasHours}
        onClick={onToggle}
      >
        <span className="block break-words text-xs text-foreground">{comfort.text}</span>
        {comfort.motion && <span className="block break-words text-xs text-muted-foreground">{comfort.motion}</span>}
      </button>
      {open && hasHours && (
        <ul id={listId} data-testid="plan-comfort-hours" className="mt-1 space-y-0.5 text-xs text-muted-foreground">
          {comfort.hours.map((h) => (
            <li key={h.at} className="break-words">
              {h.line}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
