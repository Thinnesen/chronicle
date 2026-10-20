import { useQuery } from "@tanstack/react-query";

import { workflows } from "../api/client";
import type { WorkflowSummary } from "../gen/chronicle/v1/workflow_pb";
import { lampKind } from "./Lamp";
import "./RecentRuns.css";

const shown = 10;

// useRecentRuns loads the newest runs of a namespace for the health marks of templates and schedules.
export function useRecentRuns(namespace: string) {
  return useQuery({
    queryKey: ["runs", namespace, "recent"],
    queryFn: () => workflows.listWorkflows({ namespace, limit: 200, phases: [] }),
    refetchInterval: 30_000,
  });
}

// RecentRuns shows the last runs of a template or a schedule as marks, the newest on the right. It also shows the
// share that succeeded. A broken pipeline stands out in a list at once.
export function RecentRuns({ runs }: { runs: WorkflowSummary[] }) {
  const last = runs.slice(0, shown).reverse();
  if (last.length === 0) return <span className="recent-none">No runs yet</span>;
  const finished = last.filter((r) => ["proceed", "danger"].includes(lampKind(r.phase || "Pending")));
  const ok = finished.filter((r) => lampKind(r.phase) === "proceed").length;
  const label = `${last.length} recent ${last.length === 1 ? "run" : "runs"}, ${ok} of ${finished.length} finished succeeded`;
  return (
    <span className="recent">
      <span className="recent-marks" role="img" aria-label={label} title={label}>
        {Array.from({ length: shown - last.length }, (_, i) => (
          <span key={i} className="recent-mark recent-empty" />
        ))}
        {last.map((r) => (
          <span key={r.uid} className={`recent-mark recent-${lampKind(r.phase || "Pending")}`} />
        ))}
      </span>
      {/* The rate keeps its width without a finished run, so that the strips of a list line up. */}
      <span className="recent-rate">{finished.length > 0 && `${Math.round((100 * ok) / finished.length)} %`}</span>
    </span>
  );
}
