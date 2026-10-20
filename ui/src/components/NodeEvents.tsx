import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, Info } from "lucide-react";

import { workflows } from "../api/client";
import { errorText } from "../lib/errors";
import { ago, toDate } from "../lib/time";
import "./NodeEvents.css";

// NodeEvents lists the Kubernetes events of the Pod of a node, for example why it waits to be scheduled.
export function NodeEvents({ namespace, workflow, node, live }: { namespace: string; workflow: string; node: string; live: boolean }) {
  const { data, error, isPending } = useQuery({
    queryKey: ["events", namespace, workflow, node],
    queryFn: () => workflows.listNodeEvents({ namespace, workflow, node }),
    refetchInterval: live ? 5000 : false,
  });
  if (isPending) return <p className="quiet">Loading the events…</p>;
  if (error && !data) return <p role="alert">Could not load the events: {errorText(error)}</p>;
  const events = data?.events ?? [];
  if (events.length === 0) {
    return <p className="quiet">No events. Kubernetes keeps the events of a Pod for about one hour.</p>;
  }
  return (
    <ol className="events">
      {events.map((e, i) => {
        const seen = toDate(e.lastSeen);
        const warning = e.type === "Warning";
        return (
          <li key={`${e.reason}-${i}`} className={warning ? "event is-warning" : "event"}>
            {warning ? <AlertTriangle aria-label="Warning" /> : <Info aria-label="Normal" />}
            <div className="event-text">
              <span className="event-head">
                <strong>{e.reason}</strong>
                {e.count > 1 && <span className="quiet">{e.count} times</span>}
                {seen && <time className="quiet" title={seen.toLocaleString()}>{ago(seen)}</time>}
              </span>
              <span className="event-message">{e.message}</span>
            </div>
          </li>
        );
      })}
    </ol>
  );
}
