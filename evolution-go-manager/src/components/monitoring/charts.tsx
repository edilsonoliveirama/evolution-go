/**
 * Small dependency-free charts for the Metrics page.
 */

import { nf } from './format';

interface BarPoint {
  label: string;
  tooltip: string;
  in: number;
  out: number;
}

/** Received/sent messages per day or hour, side by side. */
export function MessagesChart({ points, labelEvery = 1 }: { points: BarPoint[]; labelEvery?: number }) {
  const max = Math.max(1, ...points.map((p) => Math.max(p.in, p.out)));
  return (
    <div>
      <div className="flex h-48 items-end gap-[2px]">
        {points.map((p) => (
          <div key={p.tooltip} className="flex h-full flex-1 items-end justify-center gap-[1px]" title={p.tooltip}>
            <div className="w-1/2 rounded-t bg-green-500" style={{ height: `${(p.in / max) * 100}%` }} />
            <div className="w-1/2 rounded-t bg-primary" style={{ height: `${(p.out / max) * 100}%` }} />
          </div>
        ))}
      </div>
      <div className="mt-1 flex gap-[2px]">
        {points.map((p, i) => (
          <div key={p.tooltip} className="flex-1 truncate text-center text-[10px] text-muted-foreground">
            {i % labelEvery === 0 ? p.label : ''}
          </div>
        ))}
      </div>
      <div className="mt-3 flex gap-4 text-xs text-muted-foreground">
        <span className="flex items-center gap-1">
          <span className="h-2 w-2 rounded-sm bg-green-500" /> Recebidas
        </span>
        <span className="flex items-center gap-1">
          <span className="h-2 w-2 rounded-sm bg-primary" /> Enviadas
        </span>
      </div>
    </div>
  );
}

const WEEKDAYS = ['Seg', 'Ter', 'Qua', 'Qui', 'Sex', 'Sáb', 'Dom'];

/** Received messages per weekday (1 = Monday) and hour. */
export function Heatmap({ cells }: { cells: { dow: number; hour: number; count: number }[] }) {
  const grid = new Map(cells.map((c) => [`${c.dow}-${c.hour}`, c.count]));
  const max = Math.max(1, ...cells.map((c) => c.count));
  return (
    <div className="overflow-x-auto">
      <div className="min-w-[520px]">
        {WEEKDAYS.map((name, i) => (
          <div key={name} className="mb-[3px] flex items-center gap-[3px]">
            <span className="w-8 shrink-0 text-[10px] text-muted-foreground">{name}</span>
            {Array.from({ length: 24 }, (_, hour) => {
              const count = grid.get(`${i + 1}-${hour}`) || 0;
              return (
                <div
                  key={hour}
                  title={`${name} ${hour}h: ${nf(count)} mensagens`}
                  className={count ? 'h-4 flex-1 rounded-sm bg-primary' : 'h-4 flex-1 rounded-sm bg-muted'}
                  style={count ? { opacity: 0.15 + 0.85 * (count / max) } : undefined}
                />
              );
            })}
          </div>
        ))}
        <div className="flex gap-[3px] pl-[35px]">
          {Array.from({ length: 24 }, (_, hour) => (
            <span key={hour} className="flex-1 text-center text-[10px] text-muted-foreground">
              {hour % 3 === 0 ? hour : ''}
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}

/** Horizontal bars, e.g. messages per type. */
export function BarList({ items }: { items: { label: string; value: number; sub?: string }[] }) {
  const max = Math.max(1, ...items.map((i) => i.value));
  if (items.length === 0) {
    return <p className="text-sm text-muted-foreground">Sem dados no período.</p>;
  }
  return (
    <div className="space-y-2">
      {items.map((item) => (
        <div key={item.label} className="flex items-center gap-3 text-sm">
          <span className="w-28 shrink-0 truncate text-muted-foreground">{item.label}</span>
          <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
            <div className="h-full rounded-full bg-primary" style={{ width: `${(item.value / max) * 100}%` }} />
          </div>
          <span className="w-20 shrink-0 text-right font-semibold tabular-nums" title={item.sub}>
            {nf(item.value)}
          </span>
        </div>
      ))}
    </div>
  );
}
