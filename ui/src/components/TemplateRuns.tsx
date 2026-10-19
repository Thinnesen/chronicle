import { Link } from "@tanstack/react-router";

import type { WorkflowSummary } from "../gen/chronicle/v1/workflow_pb";
import { ago, duration, toDate } from "../lib/time";
import { Lamp, lampKind } from "./Lamp";
import "./TemplateRuns.css";

const shown = 10;

// TemplateRuns is the panel with the last runs of a template.
export function TemplateRuns({ namespace, runs, now }: { namespace: string; runs: WorkflowSummary[]; now: number }) {
  return (
    <section className="panel template-runs">
      <h2>Recent runs</h2>
      {runs.length === 0 && <p className="quiet">No runs of this template yet.</p>}
      <RunBars namespace={namespace} runs={runs} now={now} />
    </section>
  );
}

// RunBars lists the last runs of a template or a schedule with a bar for the duration of each, so that a slow or
// failed run stands out against the others.
export function RunBars({ namespace, runs, now }: { namespace: string; runs: WorkflowSummary[]; now: number }) {
  const last = runs.slice(0, shown).map((w) => {
    const start = toDate(w.startedAt);
    const end = toDate(w.finishedAt)?.getTime() ?? now;
    return { w, start, took: start ? end - start.getTime() : 0 };
  });
  const longest = Math.max(1, ...last.map((r) => r.took));
  return (
    <ol className="run-bars">
      {last.map(({ w, start, took }) => (
        <li key={w.uid}>
          <Lamp phase={w.phase || "Pending"} />
          <Link to="/ns/$namespace/runs/$name" params={{ namespace, name: w.name }}>
            {w.name}
          </Link>
          <span className="quiet">{start ? ago(start, new Date(now)) : "not started"}</span>
          <span className="run-bar" aria-hidden="true">
            <span className={`run-bar-fill run-bar-${lampKind(w.phase || "Pending")}`} style={{ width: `${(100 * took) / longest}%` }} />
          </span>
          <span className="run-took">{start ? duration(took) : "–"}</span>
        </li>
      ))}
    </ol>
  );
}
