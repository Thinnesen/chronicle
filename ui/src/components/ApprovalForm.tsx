import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { workflows } from "../api/client";
import { actionError } from "../lib/errors";
import { templateOf, type NodeStatus, type Workflow } from "../lib/workflow";
import { ConfirmDialog } from "./ConfirmDialog";
import { useCanEdit } from "./RunActions";
import { useToast } from "./Toast";

// ApprovalForm decides a waiting suspend node. The output parameters of its template are the fields.
export function ApprovalForm({ wf, node }: { wf: Workflow; node: NodeStatus }) {
  const { name, namespace } = wf.metadata;
  const canEdit = useCanEdit(namespace);
  const toast = useToast();
  const client = useQueryClient();
  const outputs = templateOf(wf, node)?.outputs?.parameters ?? [];
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(outputs.map((p) => [p.name, p.default ?? ""])));
  const [message, setMessage] = useState("");
  const [rejecting, setRejecting] = useState(false);
  const decide = useMutation({
    mutationFn: (approve: boolean) =>
      workflows.approveNode({ namespace, name, node: node.id, approve, parameters: approve ? values : {}, message }).then(() => approve),
    onSuccess: (approve) => {
      toast(`${approve ? "Approved" : "Rejected"} ${node.displayName ?? node.name}`);
      void client.invalidateQueries({ queryKey: ["run", namespace, name] });
    },
    onError: (err) => toast(actionError("approve nodes", namespace, err), "error"),
  });
  const decided = wf.spec.approvals?.some((a) => a.node === node.id || a.node === node.name);
  if (node.type !== "Suspend" || node.phase !== "Running" || decided) return null;
  if (!canEdit) return <p className="quiet">This node waits for a decision by an editor of {namespace}.</p>;
  return (
    <form
      className="approval"
      onSubmit={(e) => {
        e.preventDefault();
        decide.mutate(true);
      }}
    >
      <h3>This node waits for your decision</h3>
      {outputs.map((p) => (
        <label key={p.name} className="field">
          <span>{p.name}</span>
          <input value={values[p.name] ?? ""} onChange={(e) => setValues({ ...values, [p.name]: e.target.value })} />
          {p.description && <small className="quiet">{p.description}</small>}
        </label>
      ))}
      <label className="field">
        <span>Comment for the run history</span>
        <input value={message} onChange={(e) => setMessage(e.target.value)} placeholder="Optional" />
      </label>
      <div className="approval-buttons">
        <button type="submit" className="button button-primary" disabled={decide.isPending}>
          Approve
        </button>
        <button type="button" className="button" onClick={() => setRejecting(true)} disabled={decide.isPending}>
          Reject
        </button>
      </div>
      <ConfirmDialog
        open={rejecting}
        onOpenChange={setRejecting}
        title="Reject this node?"
        confirm="Reject"
        danger
        onConfirm={() => decide.mutate(false)}
      >
        The node fails, and the run goes on as for any failed node.
      </ConfirmDialog>
    </form>
  );
}
