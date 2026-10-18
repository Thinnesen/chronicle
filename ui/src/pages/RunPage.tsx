import { useQuery } from "@tanstack/react-query";

import { workflows } from "../api/client";
import { Lamp } from "../components/Lamp";
import { runRoute } from "../router";

// RunPage shows one run. The graph, the timeline and the logs come in the next step (#19).
export function RunPage() {
  const { namespace, name } = runRoute.useParams();
  const { data, error } = useQuery({
    queryKey: ["run", namespace, name],
    queryFn: () => workflows.getWorkflow({ namespace, name }),
    refetchInterval: 3000,
  });
  if (error) return <p role="alert">Could not load the run: {error.message}</p>;
  if (!data?.summary) return <p className="quiet">Loading the run…</p>;
  const s = data.summary;
  return (
    <section>
      <h1 className="page-title">
        <Lamp phase={s.phase || "Pending"} size={16} /> {s.name}
      </h1>
      <p className="quiet">
        {s.phase || "Pending"}
        {s.message && `: ${s.message}`}
      </p>
    </section>
  );
}
