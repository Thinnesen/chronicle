import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Hand } from "lucide-react";
import { Fragment } from "react";

import { workflows } from "../api/client";

const named = 3;

// WaitingRuns names the runs of a namespace, or of all namespaces without one, that wait for an approval, because their
// phase stays Running. It reads all running runs, so that it also finds a run that has waited for days.
export function WaitingRuns({ namespace }: { namespace?: string }) {
  const { data } = useQuery({
    queryKey: ["runs", namespace ?? "*", "Running", "waiting"],
    queryFn: () => workflows.listWorkflows({ namespace, allNamespaces: !namespace, limit: 200, phases: ["Running"] }),
    refetchInterval: 10_000,
  });
  const waiting = data?.workflows.filter((w) => w.approvalsWaiting > 0) ?? [];
  if (waiting.length === 0) return null;
  const rest = waiting.length - named;
  return (
    <div className="approval-notice waiting-runs" role="status" aria-label="Runs that wait for an approval">
      <Hand aria-hidden="true" />
      <span>
        {waiting.slice(0, named).map((w, i) => (
          <Fragment key={w.uid}>
            {i > 0 && ", "}
            <Link to="/ns/$namespace/runs/$name" params={{ namespace: w.namespace, name: w.name }}>
              {w.name}
            </Link>
          </Fragment>
        ))}
        {rest > 0 && ` and ${rest} more`}
        {waiting.length === 1 ? " waits for an approval." : " wait for an approval."}
      </span>
    </div>
  );
}
