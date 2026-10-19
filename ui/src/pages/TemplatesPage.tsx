import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";

import { templates } from "../api/client";
import { ClusterBadge, ManagedBadge } from "../components/Badge";
import { RecentRuns, useRecentRuns } from "../components/RecentRuns";
import { templatesRoute } from "../router";

// TemplatesPage lists the templates that the user can run in the namespace, cluster templates too.
export function TemplatesPage() {
  const { namespace } = templatesRoute.useParams();
  const { data, error, isPending } = useQuery({ queryKey: ["templates", namespace], queryFn: () => templates.listTemplates({ namespace }) });
  const recent = useRecentRuns(namespace);
  return (
    <section>
      <header className="page-head">
        <div>
          <h1>Templates</h1>
          <p className="quiet">Workflow templates that you can run in {namespace}, and cluster templates.</p>
        </div>
      </header>
      {isPending && <p className="quiet">Loading the templates…</p>}
      {error && <p role="alert">Could not load the templates: {error.message}</p>}
      {data?.templates.length === 0 && <p className="quiet">No templates yet. Add a WorkflowTemplate to the namespace, for example through Git.</p>}
      {data && data.templates.length > 0 && (
        <ul className="item-list">
          {data.templates.map((t) => (
            <li key={`${t.clusterScope}/${t.name}`}>
              <span className="item-title">
                <Link to="/ns/$namespace/templates/$name" params={{ namespace, name: t.name }} search={{ cluster: t.clusterScope || undefined }}>
                  {t.name}
                </Link>
                {t.clusterScope && <ClusterBadge />}
                <ManagedBadge tool={t.managedBy} />
              </span>
              <p className="item-note">{t.description || `Starts with ${t.entrypoint}`}</p>
              <span className="item-side">
                <RecentRuns runs={recent.data?.workflows.filter((w) => w.template === t.name) ?? []} />
                <span className="item-meta">
                  {t.parameters.length === 0 ? "No parameters" : t.parameters.length === 1 ? "1 parameter" : `${t.parameters.length} parameters`}
                </span>
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
