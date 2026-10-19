import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";

import { namespaces, workflows } from "../api/client";
import { actionError } from "../lib/errors";
import type { Workflow } from "../lib/workflow";
import { ConfirmDialog } from "./ConfirmDialog";
import { RerunDialog } from "./RerunDialog";
import { useToast } from "./Toast";
import "./RunActions.css";

type Action = "suspend" | "resume" | "stop" | "terminate" | "delete" | "retry";

const done: Record<Action, string> = {
  suspend: "Suspended",
  resume: "Resumed",
  stop: "Stopping",
  terminate: "Terminating",
  delete: "Deleted",
  retry: "Retrying",
};

// useCanEdit tells if the user can change runs in the namespace. The server checks again on each call.
export function useCanEdit(namespace: string): boolean {
  const { data } = useQuery({ queryKey: ["namespaces"], queryFn: () => namespaces.listNamespaces({}) });
  return data?.namespaces.find((n) => n.name === namespace)?.canSubmit ?? false;
}

// RunActions are the controls of a run. Only the actions that fit the phase show.
export function RunActions({ wf, phase, archived = false }: { wf: Workflow; phase: string; archived?: boolean }) {
  const { name, namespace } = wf.metadata;
  const canEdit = useCanEdit(namespace);
  const toast = useToast();
  const client = useQueryClient();
  const navigate = useNavigate();
  const [confirm, setConfirm] = useState<Action | null>(null);
  const [rerunning, setRerunning] = useState(false);
  const rerun = useMutation({
    mutationFn: (parameters: Record<string, string>) => workflows.rerunWorkflow({ namespace, name, parameters }),
    onSuccess: (resp) => {
      const next = resp.summary?.name ?? "";
      toast(`Started ${next}`);
      void navigate({ to: "/ns/$namespace/runs/$name", params: { namespace, name: next } });
    },
    onError: (err) => toast(actionError("re-run runs", namespace, err), "error"),
  });
  const run = useMutation({
    mutationFn: async (a: Action) => {
      const req = { namespace, name };
      if (a === "suspend") await workflows.suspendWorkflow(req);
      if (a === "resume") await workflows.resumeWorkflow(req);
      if (a === "stop") await workflows.stopWorkflow(req);
      if (a === "terminate") await workflows.terminateWorkflow(req);
      if (a === "delete") await workflows.deleteWorkflow(req);
      if (a === "retry") await workflows.retryWorkflow(req);
      return a;
    },
    onSuccess: (a) => {
      toast(`${done[a]} ${name}`);
      if (a === "delete") {
        void navigate({ to: "/ns/$namespace", params: { namespace } });
        return;
      }
      void client.invalidateQueries({ queryKey: ["run", namespace, name] });
    },
    onError: (err, a) => toast(actionError(`${a} runs`, namespace, err), "error"),
  });
  if (!canEdit) return null;
  if (archived) {
    // An archived run has no Workflow anymore. Only a re-run makes sense.
    return (
      <div className="run-actions" role="group" aria-label="Run actions">
        <button type="button" className="button" onClick={() => setRerunning(true)}>
          Re-run
        </button>
        {rerunning && <RerunDialog wf={wf} open={rerunning} onOpenChange={setRerunning} onRerun={(p) => rerun.mutate(p)} />}
      </div>
    );
  }
  const active = !["Succeeded", "Failed", "Error"].includes(phase);
  const stopping = !!wf.spec.shutdown;
  return (
    <div className="run-actions" role="group" aria-label="Run actions">
      {active && !wf.spec.suspend && !stopping && (
        <button type="button" className="button" onClick={() => run.mutate("suspend")} disabled={run.isPending}>
          Suspend
        </button>
      )}
      {active && wf.spec.suspend && (
        <button type="button" className="button button-primary" onClick={() => run.mutate("resume")} disabled={run.isPending}>
          Resume
        </button>
      )}
      {active && !stopping && (
        <button type="button" className="button" onClick={() => setConfirm("stop")}>
          Stop
        </button>
      )}
      {active && wf.spec.shutdown !== "Terminate" && (
        <button type="button" className="button" onClick={() => setConfirm("terminate")}>
          Terminate
        </button>
      )}
      {(phase === "Failed" || phase === "Error") && (
        <button type="button" className="button button-primary" onClick={() => run.mutate("retry")} disabled={run.isPending}>
          Retry
        </button>
      )}
      {!active && (
        <button type="button" className="button" onClick={() => setRerunning(true)}>
          Re-run
        </button>
      )}
      {!active && (
        <button type="button" className="button" onClick={() => setConfirm("delete")}>
          Delete
        </button>
      )}
      {rerunning && <RerunDialog wf={wf} open={rerunning} onOpenChange={setRerunning} onRerun={(p) => rerun.mutate(p)} />}
      <ConfirmDialog
        open={confirm === "stop"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={`Stop ${name}?`}
        confirm="Stop the run"
        danger
        onConfirm={() => run.mutate("stop")}
      >
        The running nodes fail and their Pods are deleted. The exit handler still runs.
      </ConfirmDialog>
      <ConfirmDialog
        open={confirm === "terminate"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={`Terminate ${name}?`}
        confirm="Terminate the run"
        danger
        onConfirm={() => run.mutate("terminate")}
      >
        The running nodes fail at once and their Pods are deleted. The exit handler does not run.
      </ConfirmDialog>
      <ConfirmDialog
        open={confirm === "delete"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={`Delete ${name}?`}
        confirm="Delete the run"
        danger
        onConfirm={() => run.mutate("delete")}
      >
        The run, its Pods and their logs are deleted from the cluster.
        {wf.status?.conditions?.some((c) => c.type === "Archived" && c.status === "True") &&
          " The archive keeps its record, so the run stays in the history with the mark Archived."}
      </ConfirmDialog>
    </div>
  );
}
