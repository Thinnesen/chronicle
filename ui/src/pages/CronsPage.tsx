import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";

import { crons } from "../api/client";
import { ManagedBadge } from "../components/Badge";
import { readable, relative } from "../lib/cron";
import { toDate } from "../lib/time";
import { useNow } from "../lib/useNow";
import { cronsRoute } from "../router";

// CronsPage lists the schedules of the namespace with their next run.
export function CronsPage() {
  const { namespace } = cronsRoute.useParams();
  const now = useNow();
  const { data, error, isPending } = useQuery({
    queryKey: ["crons", namespace],
    queryFn: () => crons.listCronWorkflows({ namespace }),
    refetchInterval: 30_000,
  });
  return (
    <section>
      <h1 className="page-title">Cron workflows in {namespace}</h1>
      {isPending && <p className="quiet">Loading the cron workflows…</p>}
      {error && <p role="alert">Could not load the cron workflows: {error.message}</p>}
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
                </p>
                <span className="item-side">{c.suspend ? "Suspended" : next ? `Next run ${relative(next, now)}` : "No run planned"}</span>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
