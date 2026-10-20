import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";

import { crons } from "../api/client";
import { ManagedBadge } from "../components/Badge";
import { RecentRuns, useRecentRuns } from "../components/RecentRuns";
import { RefreshError } from "../components/RefreshError";
import { readable, relative } from "../lib/cron";
import { toDate } from "../lib/time";
import { useNow } from "../lib/useNow";
import { cronsRoute } from "../router";
import { useTitle } from "../lib/useTitle";

// CronsPage lists the schedules of the namespace with their next run.
export function CronsPage() {
  const { namespace } = cronsRoute.useParams();
  useTitle(`Schedules in ${namespace}`);
  const now = useNow();
  const recent = useRecentRuns(namespace);
  const { data, error, isPending } = useQuery({
    queryKey: ["crons", namespace],
    queryFn: () => crons.listCronWorkflows({ namespace }),
    refetchInterval: 30_000,
  });
  return (
    <section>
      <header className="page-head">
        <div>
          <h1>Cron</h1>
          <p className="quiet">Schedules that start runs in {namespace}.</p>
        </div>
      </header>
      {isPending && <p className="quiet">Loading the cron workflows…</p>}
      {error && !data && <p role="alert">Could not load the cron workflows: {error.message}</p>}
      {data && <RefreshError error={error} />}
      {data?.cronWorkflows.length === 0 && <p className="quiet">No cron workflows yet. Add a CronWorkflow to run a template on a schedule.</p>}
      {data && data.cronWorkflows.length > 0 && (
        <ul className="item-list">
          {data.cronWorkflows.map((c) => {
            const next = toDate(c.nextScheduledAt);
            return (
              <li key={c.name}>
                <span className="item-title">
                  <Link to="/ns/$namespace/cron/$name" params={{ namespace, name: c.name }}>
                    {c.name}
                  </Link>
                  <ManagedBadge tool={c.managedBy} />
                </span>
                <p className="item-note">
                  {c.schedules.map(readable).join("; ")}
                  {c.timezone && ` (${c.timezone})`}
                  {c.description && `. ${c.description}`}
                </p>
                <span className="item-side">
                  <RecentRuns runs={recent.data?.workflows.filter((w) => w.labels["chronicle.thinnesen.com/cron-workflow"] === c.name) ?? []} />
                  <span className="item-meta">{c.suspend ? "Suspended" : next ? `Next run ${relative(next, now)}` : "No run planned"}</span>
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
