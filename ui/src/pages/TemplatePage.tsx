import { useQuery } from "@tanstack/react-query";

import { templates } from "../api/client";
import { ClusterBadge, ManagedBadge } from "../components/Badge";
import { RunForm } from "../components/RunForm";
import { useCanEdit } from "../components/RunActions";
import { YamlView } from "../components/YamlView";
import { templateRoute } from "../router";

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
      <div className="two-columns">
        {canRun ? <RunForm namespace={namespace} template={t} /> : <p className="panel quiet">Only editors of {namespace} can start runs.</p>}
        <YamlView value={data.template} label={`The YAML of ${t.name}`} />
      </div>
    </section>
  );
}
