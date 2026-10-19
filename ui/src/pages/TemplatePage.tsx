import { useQuery } from "@tanstack/react-query";
import { lazy, Suspense } from "react";

import { templates } from "../api/client";
import { ClusterBadge, ManagedBadge } from "../components/Badge";
import { RunForm } from "../components/RunForm";
import { useCanEdit } from "../components/RunActions";
import { YamlView } from "../components/YamlView";
import { previewOf, type Workflow } from "../lib/workflow";
import { templateRoute } from "../router";

const RunGraph = lazy(() => import("../components/RunGraph").then((m) => ({ default: m.RunGraph })));

// TemplatePage shows a template and lets an editor start a run of it.
export function TemplatePage() {
  const { namespace, name } = templateRoute.useParams();
  const { cluster } = templateRoute.useSearch();
  const canRun = useCanEdit(namespace);
  const { data, error } = useQuery({
    queryKey: ["template", namespace, name, !!cluster],
    queryFn: () => templates.getTemplate({ namespace, name, clusterScope: !!cluster }),
  });
  if (error) return <p role="alert">Could not load the template: {error.message}</p>;
  if (!data?.summary) return <p className="quiet">Loading the template…</p>;
  const t = data.summary;
  const plan = data.template ? previewOf(data.template as unknown as Parameters<typeof previewOf>[0]) : undefined;
  return (
    <section>
      <header className="page-head">
        <div className="item-title">
          <h1 className="mono">{t.name}</h1>
          {t.clusterScope && <ClusterBadge />}
          <ManagedBadge tool={t.managedBy} />
        </div>
      </header>
      {t.description && <p className="lead">{t.description}</p>}
      {plan && <Plan wf={plan} />}
      <div className="two-columns">
        {canRun ? <RunForm namespace={namespace} template={t} /> : <p className="panel quiet">Only editors of {namespace} can start runs.</p>}
        <YamlView value={data.template} label={`The YAML of ${t.name}`} />
      </div>
    </section>
  );
}

// Plan shows the graph of a template before it runs.
function Plan({ wf }: { wf: Workflow }) {
  return (
    <section className="card template-plan" aria-label="Steps of the template">
      <Suspense fallback={<div className="run-graph-placeholder" />}>
        <RunGraph wf={wf} now={0} onSelect={() => undefined} preview />
      </Suspense>
    </section>
  );
}
