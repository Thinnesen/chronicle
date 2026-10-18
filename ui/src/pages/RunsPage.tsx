import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";

import { workflows } from "../api/client";
import { Lamp } from "../components/Lamp";
import { ProgressTrack } from "../components/ProgressTrack";
import { clock, duration, toDate } from "../lib/time";
import { runsRoute } from "../router";
import "./RunsPage.css";

// RunsPage is the departure board of a namespace: the newest runs first, updated every few seconds.
export function RunsPage() {
  const { namespace } = runsRoute.useParams();
  const { data, error, isPending } = useQuery({
    queryKey: ["runs", namespace],
    queryFn: () => workflows.listWorkflows({ namespace, limit: 200 }),
    refetchInterval: 5000,
  });
  return (
    <section>
      <h1 className="page-title">Runs in {namespace}</h1>
      {isPending && <p className="quiet">Loading the runs…</p>}
      {error && <p role="alert">Could not load the runs: {error.message}</p>}
      {data && data.workflows.length === 0 && <p className="quiet">No runs yet. Submit one with the CLI or from a template.</p>}
      {data && data.workflows.length > 0 && (
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
            {data.workflows.map((w) => {
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
