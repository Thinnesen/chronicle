import { lampKind, type LampKind } from "./Lamp";
import "./ActivityStrip.css";

const hours = 24;
const slots = 48;
const slotMs = (hours * 3_600_000) / slots;
// The order of the stack from the bottom: a failure is on top, where it is easy to see.
const stack: LampKind[] = ["proceed", "idle", "caution", "running", "danger"];

export interface Started {
  phase: string;
  start?: Date;
}

// bucket counts the runs of the last 24 hours in slots of 30 minutes, by the kind of their phase.
export function bucket(runs: Started[], now: number): Record<LampKind, number>[] {
  const out = Array.from({ length: slots }, () => ({ proceed: 0, running: 0, danger: 0, caution: 0, idle: 0 }));
  for (const r of runs) {
    if (!r.start) continue;
    const i = slots - 1 - Math.floor((now - r.start.getTime()) / slotMs);
    const slot = out[i];
    if (slot) slot[lampKind(r.phase || "Pending")]++;
  }
  return out;
}

// ActivityStrip shows the runs of the last 24 hours as stacked bars, with the totals next to it. partial means that the
// runs are only the newest part of the last 24 hours, so the totals are lower bounds.
export function ActivityStrip({ runs, now, partial = false }: { runs: Started[]; now: number; partial?: boolean }) {
  const slotsOf = bucket(runs, now);
  const max = Math.max(1, ...slotsOf.map((s) => stack.reduce((n, k) => n + s[k], 0)));
  const total = slotsOf.reduce((n, s) => n + stack.reduce((m, k) => m + s[k], 0), 0);
  const failed = slotsOf.reduce((n, s) => n + s.danger, 0);
  const finished = slotsOf.reduce((n, s) => n + s.proceed + s.danger, 0);
  const rate = finished > 0 ? Math.round((100 * (finished - failed)) / finished) : undefined;
  return (
    <section className="activity card" aria-label="Runs in the last 24 hours">
      <dl className="activity-stats">
        <div>
          <dt>Runs in 24 h</dt>
          <dd title={partial ? `Only the newest ${runs.length} runs count` : undefined}>
            {total}
            {partial && "+"}
          </dd>
        </div>
        <div>
          <dt>Failed</dt>
          <dd className={failed > 0 ? "activity-bad" : undefined}>
            {failed}
            {partial && "+"}
          </dd>
        </div>
        <div>
          <dt>Success rate</dt>
          <dd>{rate === undefined ? "–" : `${rate} %`}</dd>
        </div>
      </dl>
      <div className="activity-chart">
        <svg viewBox={`0 0 ${slots * 10} 48`} preserveAspectRatio="none" role="img" aria-label={`${total} runs started in the last 24 hours, ${failed} failed`}>
          {slotsOf.map((s, i) => {
            let y = 48;
            return (
              <g key={i}>
                <rect x={i * 10 + 1} y={0} width={8} height={48} className="activity-slot" />
                {stack.map((k) => {
                  if (s[k] === 0) return null;
                  const h = Math.max(3, (44 * s[k]) / max);
                  y -= h;
                  return <rect key={k} x={i * 10 + 1} y={y} width={8} height={h} rx={1.5} className={`activity-bar activity-${k}`} />;
                })}
              </g>
            );
          })}
        </svg>
        <div className="activity-axis" aria-hidden="true">
          <span>24 h ago</span>
          <span>12 h ago</span>
          <span>Now</span>
        </div>
      </div>
    </section>
  );
}
