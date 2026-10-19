import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";

import { workflows } from "../api/client";
import { Lamp } from "../components/Lamp";
import { ProgressTrack } from "../components/ProgressTrack";
import { clock, duration, toDate } from "../lib/time";
import { runsRoute } from "../router";
import "./RunsPage.css";

// RunsPage is the departure board of a namespace: the newest runs first, updated every few seconds.
const phaseFilters = ["Running", "Suspended", "Failed", "Succeeded"];

export function RunsPage() {
  const { namespace } = runsRoute.useParams();
  const { q, phase } = runsRoute.useSearch();
  const navigate = useNavigate({ from: runsRoute.fullPath });
  const phases = phase === "Failed" ? ["Failed", "Error"] : phase ? [phase] : [];
  const { data, error, isPending } = useQuery({
    queryKey: ["runs", namespace, phase],
    queryFn: () => workflows.listWorkflows({ namespace, limit: 200, phases }),
    refetchInterval: 5000,
  });
  const term = (q ?? "").toLowerCase();
  const shown = data?.workflows.filter((w) => w.name.toLowerCase().includes(term) || w.template.toLowerCase().includes(term));
  return (
    <section>
      <h1 className="page-title">Runs in {namespace}</h1>
      <div className="board-filters">
        <label className="search">
          <span className="visually-hidden">Search runs</span>
          <input
            type="search"
            placeholder="Search by run or template"
            value={q ?? ""}
            onChange={(e) => void navigate({ search: (s) => ({ ...s, q: e.target.value || undefined }), replace: true })}
          />
        </label>
        <div className="phase-filter" role="radiogroup" aria-label="Phase">
          {[undefined, ...phaseFilters].map((p) => (
            <button
              key={p ?? "all"}
              type="button"
              role="radio"
              aria-checked={phase === p}
              onClick={() => void navigate({ search: (s) => ({ ...s, phase: p }), replace: true })}
            >
              {p ?? "All"}
            </button>
          ))}
        </div>
      </div>
      {isPending && <p className="quiet">Loading the runs…</p>}
      {error && <p role="alert">Could not load the runs: {error.message}</p>}
      {shown && shown.length === 0 && (
        <p className="quiet">{q || phase ? "No run matches the filter." : "No runs yet. Submit one with the CLI or from a template."}</p>
      )}
      {shown && shown.length > 0 && (
        <table className="board">
          <thead>
            <tr>
              <th scope="col">Started</th>
              <th scope="col">Run</th>
              <th scope="col">Progress</th>
              <th scope="col">Duration</th>
              <th scope="col" className="board-message">Message</th>
            </tr>
          </thead>
          <tbody>
            {shown.map((w) => {
              const started = toDate(w.startedAt) ?? toDate(w.createdAt);
              const end = toDate(w.finishedAt) ?? new Date();
              const phase = w.phase || "Pending";
              return (
                <tr key={w.uid}>
                  <td className="board-time">{started ? clock(started) : "–"}</td>
                  <td>
                    <span className="board-run">
                      <Lamp phase={phase} />
                      <Link to="/ns/$namespace/runs/$name" params={{ namespace, name: w.name }}>
                        {w.name}
                      </Link>
                      <span className="visually-hidden">{phase}</span>
                      {w.archived && <span className="badge">Archived</span>}
                    </span>
                    {w.template && <span className="board-template">{w.template}</span>}
                  </td>
                  <td>
                    <ProgressTrack done={w.nodesDone} total={w.nodesTotal} phase={phase} />
                  </td>
                  <td className="board-duration">{started && toDate(w.startedAt) ? duration(end.getTime() - started.getTime()) : "–"}</td>
                  <td className="board-message">{w.message}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </section>
  );
}
