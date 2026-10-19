import * as Dialog from "@radix-ui/react-dialog";
import { useState } from "react";

import { runParameters, type Workflow } from "../lib/workflow";

// RerunDialog starts a new run with the spec of a run. The parameters start with the values of the run.
export function RerunDialog(props: { wf: Workflow; open: boolean; onOpenChange: (o: boolean) => void; onRerun: (params: Record<string, string>) => void }) {
  const params = runParameters(props.wf);
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(params.map((p) => [p.name, p.value ?? ""])));
  return (
    <Dialog.Root open={props.open} onOpenChange={props.onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog">
          <Dialog.Title className="dialog-title">Re-run {props.wf.metadata.name}</Dialog.Title>
          <Dialog.Description className="dialog-body">
            A new run starts with the same spec. Change the parameters that must differ.
          </Dialog.Description>
          <form
            className="run-form"
            onSubmit={(e) => {
              e.preventDefault();
              const changed = Object.fromEntries(Object.entries(values).filter(([k, v]) => params.find((p) => p.name === k)?.value !== v));
              props.onRerun(changed);
              props.onOpenChange(false);
            }}
          >
            {params.map((p) => (
              <label key={p.name} className="field">
                <span>{p.name}</span>
                <input value={values[p.name] ?? ""} onChange={(e) => setValues({ ...values, [p.name]: e.target.value })} />
              </label>
            ))}
            <div className="dialog-buttons">
              <Dialog.Close className="button">Cancel</Dialog.Close>
              <button type="submit" className="button button-primary">
                Start the new run
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
