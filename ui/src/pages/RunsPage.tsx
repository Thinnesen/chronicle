import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Plus, Search } from "lucide-react";
import { useState } from "react";

import { workflows } from "../api/client";
import { ActivityStrip } from "../components/ActivityStrip";
import { Lamp, lampKind, Status } from "../components/Lamp";
import { ProgressTrack } from "../components/ProgressTrack";
import { RefreshError } from "../components/RefreshError";
import { useCanEdit } from "../components/RunActions";
import { SubmitDialog } from "../components/SubmitDialog";
import { ago, duration, toDate } from "../lib/time";
import { useNow } from "../lib/useNow";
import { runsRoute } from "../router";
import "./RunsPage.css";

const filters = [
  { phase: undefined, label: "All" },
  { phase: "Running", label: "Running" },
  { phase: "Suspended", label: "Suspended" },
  { phase: "Failed", label: "Failed" },
  { phase: "Succeeded", label: "Succeeded" },
] as const;

function matches(phase: string | undefined, filter: string | undefined): boolean {
  if (!filter) return true;
  return filter === "Failed" ? lampKind(phase || "Pending") === "danger" : phase === filter;
}

// RunsPage lists the runs of a namespace, newest first: the activity of 24 hours, the filters and a dense table.
export function RunsPage() {
  const { namespace } = runsRoute.useParams();
  const { q, phase } = runsRoute.useSearch();
  const navigate = useNavigate({ from: runsRoute.fullPath });
  const now = useNow(true);
  const canSubmit = useCanEdit(namespace);
  const [submitting, setSubmitting] = useState(false);
  // The newest 200 runs without a filter give the counts and the activity. A phase filter asks the server, so older
  // runs are found too. "Show more" loads the next page of the table.
  const table = useInfiniteQuery({
    queryKey: ["runs", namespace, phase],
    queryFn: ({ pageParam }) =>
      workflows.listWorkflows({ namespace, limit: 200, pageToken: pageParam, phases: phase === "Failed" ? ["Failed", "Error"] : phase ? [phase] : [] }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
    refetchInterval: 5000,
  });
  const all = useQuery({
    queryKey: ["runs", namespace, "counts"],
    queryFn: () => workflows.listWorkflows({ namespace, limit: 200, phases: [] }),
    refetchInterval: 5000,
    enabled: !!phase,
  });
  const term = (q ?? "").toLowerCase();
  const shown = table.data?.pages
    .flatMap((p) => p.workflows)
    .filter((w) => w.name.toLowerCase().includes(term) || w.template.toLowerCase().includes(term));
  const first = phase ? all.data : table.data?.pages[0];
  const runs = first?.workflows ?? [];
  return (
    <section>
      <header className="page-head">
        <div>
          <h1>Runs</h1>
          <p className="quiet">All runs in {namespace}, newest first.</p>
        </div>
        {canSubmit && (
          <button type="button" className="button button-primary" onClick={() => setSubmitting(true)}>
            <Plus aria-hidden="true" />
            New run
          </button>
        )}
      </header>
      {submitting && <SubmitDialog namespace={namespace} open={submitting} onOpenChange={setSubmitting} />}
      <ActivityStrip runs={runs.map((w) => ({ phase: w.phase, start: toDate(w.startedAt) ?? toDate(w.createdAt) }))} now={now} />
      <div className="board-filters">
        <label className="search-field">
          <Search aria-hidden="true" />
          <span className="visually-hidden">Search runs</span>
          <input
            type="search"
            placeholder="Search by run or template"
            value={q ?? ""}
            onChange={(e) => void navigate({ search: (s) => ({ ...s, q: e.target.value || undefined }), replace: true })}
          />
        </label>
        <div className="chips" role="radiogroup" aria-label="Phase">
          {filters.map((f) => (
            <button
              key={f.label}
              type="button"
              role="radio"
              className="chip"
              aria-checked={phase === f.phase}
              onClick={() => void navigate({ search: (s) => ({ ...s, phase: f.phase }), replace: true })}
            >
              {f.phase && <Lamp phase={f.phase} size={12} />}
              {f.label}
              {first && <span className="chip-count">{runs.filter((w) => matches(w.phase, f.phase)).length}</span>}
            </button>
          ))}
        </div>
      </div>
      {table.isPending && <p className="quiet">Loading the runs…</p>}
      {table.error && !table.data && <p role="alert">Could not load the runs: {table.error.message}</p>}
      {table.data && <RefreshError error={table.error} />}
      {shown && shown.length === 0 && (
        <div className="empty card">
          <p>{q || phase ? "No run matches the filter." : "No runs yet."}</p>
          <p className="quiet">{q || phase ? "Change the search or choose All." : "Submit a run from a template, or with chronicle submit."}</p>
        </div>
      )}
      {shown && shown.length > 0 && (
        <div className="card board-wrap">
          <table className="board">
            <thead>
              <tr>
                <th scope="col">Status</th>
                <th scope="col">Run</th>
                <th scope="col" className="board-template">
                  Template
                </th>
                <th scope="col">Progress</th>
                <th scope="col">Started</th>
                <th scope="col" className="board-num">
                  Duration
                </th>
              </tr>
            </thead>
            <tbody>
              {shown.map((w) => {
                const started = toDate(w.startedAt);
                const created = started ?? toDate(w.createdAt);
                const end = toDate(w.finishedAt)?.getTime() ?? now;
                const p = w.phase || "Pending";
                return (
                  <tr key={w.uid}>
                    <td>
                      <Status phase={p} />
                    </td>
                    <td className="board-run">
                      <span className="board-name">
                        <Link to="/ns/$namespace/runs/$name" params={{ namespace, name: w.name }}>
                          {w.name}
                        </Link>
                        {w.archived && <span className="badge">Archived</span>}
                        {w.approvalsWaiting > 0 && <span className="badge badge-caution">Needs approval</span>}
                      </span>
                      {w.message && (
                        <span className="board-message" title={w.message}>
                          {w.message}
                        </span>
                      )}
                    </td>
                    <td className="board-template">{w.template || <span className="quiet">–</span>}</td>
                    <td>
                      <ProgressTrack done={w.nodesDone} total={w.nodesTotal} phase={p} />
                    </td>
                    <td className="board-time">{created ? <time title={created.toLocaleString()}>{ago(created, new Date(now))}</time> : "–"}</td>
                    <td className="board-num">{started ? duration(end - started.getTime()) : "–"}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {table.hasNextPage && (
            <div className="board-more">
              <button type="button" className="button" onClick={() => void table.fetchNextPage()} disabled={table.isFetchingNextPage}>
                {table.isFetchingNextPage ? "Loading…" : "Show more runs"}
              </button>
            </div>
          )}
        </div>
      )}
    </section>
  );
}
