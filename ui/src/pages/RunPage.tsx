import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { lazy, Suspense, useMemo } from "react";

import { workflows } from "../api/client";
import { Lamp } from "../components/Lamp";
import { NodePanel } from "../components/NodePanel";
import { ProgressTrack } from "../components/ProgressTrack";
import { RunActions } from "../components/RunActions";
import { Timeline } from "../components/Timeline";
import { clock, duration, toDate } from "../lib/time";
import { useNow } from "../lib/useNow";
import type { Workflow } from "../lib/workflow";
import { runRoute } from "../router";
import "./RunPage.css";

// The graph library is large, so it loads only on this page.
const RunGraph = lazy(() => import("../components/RunGraph").then((m) => ({ default: m.RunGraph })));

const finished = new Set(["Succeeded", "Failed", "Error"]);

// RunPage shows one run: the track diagram, the timeline and the selected node. It updates while the run is active.
export function RunPage() {
  const { namespace, name } = runRoute.useParams();
  const { node: selected } = runRoute.useSearch();
  const navigate = useNavigate({ from: runRoute.fullPath });
  const { data, error } = useQuery({
    queryKey: ["run", namespace, name],
    queryFn: () => workflows.getWorkflow({ namespace, name }),
    refetchInterval: (q) => (finished.has(q.state.data?.summary?.phase ?? "") ? false : 2000),
  });
  const wf = useMemo(() => (data?.workflow ? (data.workflow as unknown as Workflow) : undefined), [data]);
  const now = useNow(!finished.has(data?.summary?.phase ?? ""));
  if (error) return <p role="alert">Could not load the run: {error.message}</p>;
  if (!data?.summary || !wf) return <p className="quiet">Loading the run…</p>;
  const s = data.summary;
  const phase = s.phase || "Pending";
  const started = toDate(s.startedAt);
  const nodes = Object.values(wf.status?.nodes ?? {});
  const select = (id: string) => void navigate({ search: { node: id }, replace: true });
  return (
    <section className="run">
      <nav className="crumbs" aria-label="Breadcrumb">
        <Link to="/ns/$namespace" params={{ namespace }}>
          Runs in {namespace}
        </Link>
      </nav>
      <header className="run-head">
        <div className="run-title">
          <h1>
            <Lamp phase={phase} size={18} />
            <span>{s.name}</span>
          </h1>
          <RunActions wf={wf} phase={phase} />
        </div>
        <dl className="run-facts">
          <div>
            <dt>Phase</dt>
            <dd>{phase}</dd>
          </div>
          {started && (
            <div>
              <dt>Started</dt>
              <dd>{clock(started)}</dd>
            </div>
          )}
          {started && (
            <div>
              <dt>Duration</dt>
              <dd>{duration((toDate(s.finishedAt)?.getTime() ?? now) - started.getTime())}</dd>
            </div>
          )}
          {s.template && (
            <div>
              <dt>Template</dt>
              <dd>{s.template}</dd>
            </div>
          )}
          <div>
            <dt>Progress</dt>
            <dd>
              <ProgressTrack done={s.nodesDone} total={s.nodesTotal} phase={phase} />
            </dd>
          </div>
        </dl>
        {s.message && <p className="run-message">{s.message}</p>}
      </header>
      <Suspense fallback={<div className="run-graph-placeholder" />}>
        <RunGraph wf={wf} now={now} selected={selected} onSelect={select} />
      </Suspense>
      <div className="run-lower">
        <Timeline nodes={nodes} now={now} selected={selected} onSelect={select} />
        <NodePanel
          wf={wf}
          node={selected ? wf.status?.nodes?.[selected] : undefined}
          nodes={wf.status?.nodes ?? {}}
          namespace={namespace}
          workflow={name}
          onSelect={select}
        />
      </div>
    </section>
  );
}
