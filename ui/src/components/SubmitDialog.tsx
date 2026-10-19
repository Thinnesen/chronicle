import type { JsonObject } from "@bufbuild/protobuf";
import * as Dialog from "@radix-ui/react-dialog";
import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { parse } from "yaml";

import { workflows } from "../api/client";
import { actionError } from "../lib/errors";
import { useToast } from "./Toast";
import "./ConfirmDialog.css";

const example = `apiVersion: chronicle.thinnesen.com/v1alpha1
kind: Workflow
metadata:
  generateName: hello-
spec:
  entrypoint: main
  templates:
  - name: main
    container:
      image: busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e
      command: [echo, hello]
`;

// readWorkflow parses the YAML of a Workflow. YAML 1.2 reads only true and false as booleans, as the CLI does.
function readWorkflow(text: string): JsonObject {
  const value: unknown = parse(text);
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error("the YAML is not an object");
  return value as JsonObject;
}

// SubmitDialog starts a run from a Workflow in YAML. The server checks it with the admission webhook.
export function SubmitDialog({ namespace, open, onOpenChange }: { namespace: string; open: boolean; onOpenChange: (o: boolean) => void }) {
  const [text, setText] = useState(example);
  const [problem, setProblem] = useState("");
  const navigate = useNavigate();
  const toast = useToast();
  const submit = useMutation({
    mutationFn: (workflow: JsonObject) => workflows.submitWorkflow({ namespace, source: { case: "workflow", value: workflow } }),
    onSuccess: (resp) => {
      const name = resp.summary?.name ?? "";
      toast(`Started ${name}`);
      onOpenChange(false);
      void navigate({ to: "/ns/$namespace/runs/$name", params: { namespace, name } });
    },
    onError: (err) => setProblem(actionError("submit runs", namespace, err)),
  });
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog dialog-wide">
          <Dialog.Title className="dialog-title">New run in {namespace}</Dialog.Title>
          <Dialog.Description className="dialog-body">Paste a Workflow in YAML. The server checks it before the run starts.</Dialog.Description>
          <form
            className="run-form"
            onSubmit={(e) => {
              e.preventDefault();
              setProblem("");
              try {
                submit.mutate(readWorkflow(text));
              } catch (err) {
                setProblem(`The YAML is not valid: ${err instanceof Error ? err.message : String(err)}`);
              }
            }}
          >
            <label className="field">
              <span>Workflow</span>
              <textarea className="yaml-input" rows={16} spellCheck={false} value={text} onChange={(e) => setText(e.target.value)} />
            </label>
            {problem && (
              <p className="submit-problem" role="alert">
                {problem}
              </p>
            )}
            <div className="dialog-buttons">
              <Dialog.Close className="button">Cancel</Dialog.Close>
              <button type="submit" className="button button-primary" disabled={submit.isPending}>
                {submit.isPending ? "Starting…" : "Start the run"}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
