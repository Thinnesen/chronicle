import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { Plus, Search } from "lucide-react";
import { useState } from "react";

import { workflows } from "../api/client";
import { ActivityStrip } from "../components/ActivityStrip";
import { Lamp, lampKind, Status } from "../components/Lamp";
import { ProgressTrack } from "../components/ProgressTrack";
import { RefreshError } from "../components/RefreshError";
import { useCanEdit } from "../components/RunActions";
import { SubmitDialog } from "../components/Lazy";
import { ago, duration, toDate } from "../lib/time";
import { WaitingRuns } from "../components/WaitingRuns";
import { useNow } from "../lib/useNow";
import { runsRoute, type RunsSearch } from "../router";
import "./RunsPage.css";
import { useTitle } from "../lib/useTitle";
import { loadError } from "../lib/errors";

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

// RunsPage lists the runs of a namespace.
export function RunsPage() {
  const { namespace } = runsRoute.useParams();
  return <RunsBoard namespace={namespace} />;
}

// AllRunsPage lists the runs of all namespaces that the user can see.
export function AllRunsPage() {
  return <RunsBoard />;
}

// RunsBoard lists runs, newest first: the activity of 24 hours, the filters and a dense table. Without a namespace,
// it lists the runs of all namespaces, with a namespace column.
function RunsBoard({ namespace }: { namespace?: string }) {
  const all = !namespace;
  useTitle(all ? "Runs in all namespaces" : `Runs in ${namespace}`);
  const { q, phase } = useSearch({ strict: false }) as RunsSearch;
  const navigate = useNavigate();
  const setSearch = (change: RunsSearch) => void navigate({ to: ".", search: (s: RunsSearch) => ({ ...s, ...change }), replace: true });
  const now = useNow(true);
  const canSubmit = useCanEdit(namespace ?? "");
  const scope = { namespace, allNamespaces: all };
  const [submitting, setSubmitting] = useState(false);
  // The newest 200 runs without a filter give the counts and the activity. A phase filter asks the server, so older
  // runs are found too. "Show more" loads the next page of the table.
  const table = useInfiniteQuery({
    queryKey: ["runs", namespace ?? "*", phase],
    queryFn: ({ pageParam }) =>
      workflows.listWorkflows({ ...scope, limit: 200, pageToken: pageParam, phases: phase === "Failed" ? ["Failed", "Error"] : phase ? [phase] : [] }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
    refetchInterval: 5000,
  });
  const counts = useQuery({
    queryKey: ["runs", namespace ?? "*", "counts"],
    queryFn: () => workflows.listWorkflows({ ...scope, limit: 200, phases: [] }),
    refetchInterval: 5000,
    enabled: !!phase,
  });
  const term = (q ?? "").toLowerCase();
  const shown = table.data?.pages
    .flatMap((p) => p.workflows)
    .filter((w) => w.name.toLowerCase().includes(term) || w.template.toLowerCase().includes(term));
  const first = phase ? counts.data : table.data?.pages[0];
  const runs = first?.workflows ?? [];
  const started = runs.map((w) => ({ phase: w.phase, start: toDate(w.startedAt) ?? toDate(w.createdAt) }));
  // A full page whose oldest run is less than 24 hours old does not have all runs of the 24 hours.
  const oldest = started.at(-1)?.start;
  const partial = !!first?.nextPageToken && !!oldest && now - oldest.getTime() < 24 * 3_600_000;
  return (
    <section>
      <header className="page-head">
        <div>
          <h1>Runs</h1>
          <p className="quiet">{all ? "All runs in your namespaces, newest first." : `All runs in ${namespace}, newest first.`}</p>
        </div>
        {canSubmit && (
          <button type="button" className="button button-primary" onClick={() => setSubmitting(true)}>
            <Plus aria-hidden="true" />
            New run
          </button>
        )}
      </header>
      {submitting && namespace && <SubmitDialog namespace={namespace} open={submitting} onOpenChange={setSubmitting} />}
      <WaitingRuns namespace={namespace} />
      {!(table.error && !table.data) && <ActivityStrip runs={started} now={now} partial={partial} />}
      <div className="board-filters">
        <label className="search-field">
          <Search aria-hidden="true" />
          <span className="visually-hidden">Search runs</span>
          <input
            type="search"
            placeholder="Search by run or template"
            value={q ?? ""}
            onChange={(e) => setSearch({ q: e.target.value || undefined })}
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
              onClick={() => setSearch({ phase: f.phase })}
            >
              {f.phase && <Lamp phase={f.phase} size={12} />}
              {f.label}
              {first && <span className="chip-count">{runs.filter((w) => matches(w.phase, f.phase)).length}</span>}
            </button>
          ))}
        </div>
      </div>
      {table.isPending && <p className="quiet">Loading the runs…</p>}
      {table.error && !table.data && <p role="alert">{loadError("runs", namespace ?? "all namespaces", table.error)}</p>}
      {table.data && <RefreshError error={table.error} />}
      {table.data?.pages.some((p) => p.archiveUnavailable) && (
        <p className="refresh-error" role="status">
          The archive did not answer. The list shows only the runs in the cluster.
        </p>
      )}
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
                {all && (
                  <th scope="col" className="board-ns">
                    Namespace
                  </th>
                )}
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
                        <Link to="/ns/$namespace/runs/$name" params={{ namespace: w.namespace, name: w.name }}>
                          {w.name}
                        </Link>
                        {all && <span className="board-ns-inline">{w.namespace}</span>}
                        {w.archived && <span className="badge">Archived</span>}
                        {w.approvalsWaiting > 0 && <span className="badge badge-caution">Needs approval</span>}
                        {w.waitingReason && (
                          <span className="badge badge-caution" title="A step does not start. Open the run to see why.">
                            {w.waitingReason}
                          </span>
                        )}
                      </span>
                      {w.message && (
                        <span className="board-message" title={w.message}>
                          {w.message}
                        </span>
                      )}
                    </td>
                    {all && (
                      <td className="board-ns">
                        <Link to="/ns/$namespace" params={{ namespace: w.namespace }}>
                          {w.namespace}
                        </Link>
                      </td>
                    )}
                    <td className="board-template">
                      {w.template ? (
                        <button
                          type="button"
                          className="board-filter"
                          aria-label={`Show the runs of ${w.template}`}
                          onClick={() => setSearch({ q: w.template })}
                        >
                          {w.template}
                        </button>
                      ) : (
                        <span className="quiet">–</span>
                      )}
                    </td>
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
