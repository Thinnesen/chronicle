import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";

import { crons, workflows } from "../api/client";
import { ManagedBadge } from "../components/Badge";
import { useCanEdit } from "../components/RunActions";
import { RunBars } from "../components/TemplateRuns";
import { useToast } from "../components/Toast";
import { YamlView } from "../components/Lazy";
import { readable, relative } from "../lib/cron";
import { actionError } from "../lib/errors";
import { ago, clock, toDate } from "../lib/time";
import { useNow } from "../lib/useNow";
import { cronRoute } from "../router";
import { useTitle } from "../lib/useTitle";
import { PageError } from "../components/PageError";

// CronPage shows a schedule, its recent runs and its YAML. An editor can suspend and resume it.
export function CronPage() {
  const { namespace, name } = cronRoute.useParams();
  useTitle(name);
  const now = useNow();
  const canEdit = useCanEdit(namespace);
  const toast = useToast();
  const client = useQueryClient();
  const { data, error } = useQuery({ queryKey: ["cron", namespace, name], queryFn: () => crons.getCronWorkflow({ namespace, name }) });
  const runs = useQuery({
    queryKey: ["runs", namespace, "cron", name],
    queryFn: () => workflows.listWorkflows({ namespace, labelSelector: `chronicle.thinnesen.com/cron-workflow=${name}`, limit: 20 }),
    refetchInterval: 10_000,
  });
  const toggle = useMutation({
    mutationFn: (suspend: boolean) =>
      (suspend ? crons.suspendCronWorkflow({ namespace, name }) : crons.resumeCronWorkflow({ namespace, name })).then(() => suspend),
    onSuccess: (suspend) => {
      toast(`${suspend ? "Suspended" : "Resumed"} ${name}`);
      void client.invalidateQueries({ queryKey: ["cron", namespace, name] });
    },
    onError: (err) => toast(actionError("change cron workflows", namespace, err), "error"),
  });
  if (error && !data) {
    return (
      <PageError error={error} what="the schedule" missing={`The schedule ${name} does not exist.`}>
        <Link to="/ns/$namespace/cron" params={{ namespace }}>
          Show the schedules of {namespace}
        </Link>
      </PageError>
    );
  }
  if (!data?.summary) return <p className="quiet">Loading the cron workflow…</p>;
  const c = data.summary;
  const next = toDate(c.nextScheduledAt);
  const last = toDate(c.lastScheduledAt);
  return (
    <section>
      <header className="page-head">
        <div className="item-title">
          <h1 className="mono">{c.name}</h1>
          <ManagedBadge tool={c.managedBy} />
        </div>
        <span className="item-title">
          {canEdit && (
            <button
              type="button"
              className="button"
              disabled={!!c.managedBy || toggle.isPending}
              title={c.managedBy ? `${c.managedBy} manages this CronWorkflow. Change it in Git.` : undefined}
              onClick={() => toggle.mutate(!c.suspend)}
            >
              {c.suspend ? "Resume" : "Suspend"}
            </button>
          )}
        </span>
      </header>
      {c.description && <p className="lead">{c.description}</p>}
      <div className="two-columns">
        <div className="panel">
          <h2>Schedule</h2>
          <dl className="facts">
            {c.schedules.map((s) => (
              <div className="fact" key={s}>
                <dt>
                  <code>{s}</code>
                </dt>
                <dd>{readable(s)}</dd>
              </div>
            ))}
            <dt>Time zone</dt>
            <dd>{c.timezone || "UTC"}</dd>
            <dt>State</dt>
            <dd>{c.suspend ? "Suspended" : "Active"}</dd>
            {next && !c.suspend && (
              <>
                <dt>Next run</dt>
                <dd>
                  {clock(next)}, {relative(next, now)}
                </dd>
              </>
            )}
            {last && (
              <>
                <dt>Last run</dt>
                <dd>{ago(last, new Date(now))}</dd>
              </>
            )}
            {c.template && (
              <>
                <dt>Template</dt>
                <dd>
                  <Link to="/ns/$namespace/templates/$name" params={{ namespace, name: c.template }}>
                    {c.template}
                  </Link>
                </dd>
              </>
            )}
          </dl>
          <h2 className="recent-title">Recent runs</h2>
          {runs.data?.workflows.length === 0 && <p className="quiet">No runs yet.</p>}
          <RunBars namespace={namespace} runs={runs.data?.workflows ?? []} now={now} />
        </div>
        <YamlView value={data.cronWorkflow} label={`The YAML of ${c.name}`} />
      </div>
    </section>
  );
}
