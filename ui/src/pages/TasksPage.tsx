import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";

import { tasks } from "../api/client";
import { Lamp } from "../components/Lamp";
import { RefreshError } from "../components/RefreshError";
import { ago, toDate } from "../lib/time";
import { useNow } from "../lib/useNow";
import { tasksRoute } from "../router";
import { useTitle } from "../lib/useTitle";
import { errorText } from "../lib/errors";

const stateFilters = { pending: "Pending", running: "Running", failed: "Failed", cancelled: "Cancelled", succeeded: "Succeeded" };

// phaseOf maps a task state to the phase that the lamps show.
function phaseOf(state: string): string {
  switch (state) {
    case "pending":
      return "Pending";
    case "running":
      return "Running";
    case "succeeded":
      return "Succeeded";
    case "cancelled":
      return "Skipped"; // the gray lamp: the task stopped, no worker failed
    default:
      return "Failed";
  }
}

// TasksPage lists the newest tasks of the task queue of the namespace: what waits for a worker, and what runs.
export function TasksPage() {
  const { namespace } = tasksRoute.useParams();
  useTitle(`Tasks in ${namespace}`);
  const { state } = tasksRoute.useSearch();
  const navigate = useNavigate({ from: tasksRoute.fullPath });
  const now = useNow();
  const states = state === "failed" ? ["failed", "error"] : state ? [state] : [];
  const { data, error, isPending } = useQuery({
    queryKey: ["tasks", namespace, state],
    queryFn: () => tasks.listTasks({ namespace, states }),
    refetchInterval: 5_000,
  });
  return (
    <section>
      <header className="page-head">
        <div>
          <h1>Tasks</h1>
          <p className="quiet">Work that task templates send to the workers of an SDK, newest first.</p>
        </div>
      </header>
      <div className="chips list-filters" role="radiogroup" aria-label="State">
        {[undefined, ...Object.keys(stateFilters)].map((s) => (
          <button
            key={s ?? "all"}
            type="button"
            role="radio"
            className="chip"
            aria-checked={state === s}
            onClick={() => void navigate({ search: { state: s }, replace: true })}
          >
            {s && <Lamp phase={phaseOf(s)} size={12} />}
            {s ? stateFilters[s as keyof typeof stateFilters] : "All"}
          </button>
        ))}
      </div>
      {isPending && <p className="quiet">Loading the tasks…</p>}
      {error && !data && <p role="alert">Could not load the tasks: {errorText(error)}</p>}
      {data && <RefreshError error={error} />}
      {data?.tasks.length === 0 && (
        <div className="empty card">
          <p>{state ? "No task matches the filter." : "No tasks yet."}</p>
          <p className="quiet">{state ? "Choose All to see every task." : "A task template sends work to the workers of an SDK. See docs/task-workers.md."}</p>
        </div>
      )}
      {data && data.tasks.length > 0 && (
        <ul className="item-list">
          {data.tasks.map((t) => {
            const created = toDate(t.createdAt);
            return (
              <li key={t.id}>
                <span className="item-title">
                  <Lamp phase={phaseOf(t.state)} />
                  <span className="mono">{t.name}</span>
                  <span className="badge">queue {t.queue}</span>
                </span>
                <p className="item-note">
                  <Link to="/ns/$namespace/runs/$name" params={{ namespace, name: t.workflow }}>
                    {t.nodeName || t.workflow}
                  </Link>
                  {t.worker && `, taken by ${t.worker}`}
                  {t.message && `: ${t.message}`}
                </p>
                <span className="item-side">
                  {t.state}
                  {created && `, ${ago(created, new Date(now))}`}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
